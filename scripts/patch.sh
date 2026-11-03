#!/bin/sh
# Patches Rex so it loads the Changes panel.
#
#   patch.sh            patch /Applications/Rex Beta.app (the official build is backed up first)
#   patch.sh --test     put a patched copy in <prefix>/test and leave /Applications alone
#   patch.sh --check    report whether the installed app is patched (exit 0 yes, 3 no)
#
# Adds a weak LC_LOAD_WEAK_DYLIB for the panel dylib and re-signs the main
# bundle ad hoc with the original entitlements plus disable-library-validation.
# The Rex server helper and Sparkle keep their own signatures. Sparkle can't
# update the patched app, so its checks are turned off; update.sh installs
# new Rex versions (the re-patch agent runs it every hour).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
PREFIX="$HOME/Library/Application Support/rexmod"
APP="${REX_APP:-/Applications/Rex Beta.app}"
BIN_REL="Contents/MacOS/Rex"
DYLIB="$PREFIX/RexModPanel.dylib"
TEAM="AG92C3PT77"
MODE="${1:-install}"

INSERT="$PREFIX/bin/insertdylib"
[ -x "$INSERT" ] || INSERT="$HERE/../dist/insertdylib"
[ -x "$INSERT" ] || { echo "insertdylib not found; run install.sh first" >&2; exit 1; }
[ -f "$DYLIB" ] || { echo "panel dylib not found at $DYLIB; run install.sh first" >&2; exit 1; }
[ -d "$APP" ] || { echo "Rex not found at $APP" >&2; exit 1; }

# Rex can ship a universal binary, and insertdylib only reads a thin one.
# The panel is arm64, so the patched app keeps only the arm64 slice.
ARCH=arm64
is_fat() { lipo -info "$1" 2>/dev/null | grep -q "^Architectures in the fat file"; }

if [ "$MODE" = "--check" ]; then
  if is_fat "$APP/$BIN_REL"; then
    # Still fat means not patched, but report it the way insertdylib does.
    T="$(mktemp)"
    lipo -thin "$ARCH" "$APP/$BIN_REL" -output "$T"
    rc=0
    "$INSERT" --check "$T" "$DYLIB" || rc=$?
    rm -f "$T"
    exit "$rc"
  fi
  exec "$INSERT" --check "$APP/$BIN_REL" "$DYLIB"
fi
# One patch at a time: a re-patch agent and an install can fire together
# after a Rex update, and two runs swapping the app at once would break it.
LOCK="$PREFIX/.patch.lock"
mkdir -p "$PREFIX"
if ! mkdir "$LOCK" 2>/dev/null; then
  if [ -n "$(find "$LOCK" -maxdepth 0 -mmin +10 2>/dev/null)" ]; then
    rm -rf "$LOCK" && mkdir "$LOCK" # left behind by a run that died
  else
    echo "another patch is running; leaving it to that one"
    exit 0
  fi
fi
STAGE=""
trap 'rm -rf "$LOCK" ${STAGE:+"$STAGE"}' EXIT
# Sparkle can't update the patched app: its installer only talks to an app
# signed by Superlogical. Once Rex is patched, turn its checks off so Rex
# doesn't show "Update failed"; rexmod's re-patch agent installs updates
# instead (update.sh). unpatch.sh turns them back on.
quiet_sparkle() {
  defaults write com.superlogical.rex SUEnableAutomaticChecks -bool false
  defaults write com.superlogical.rex SUAutomaticallyUpdate -bool false
}
# Checked inside the lock: the other run may have just patched it.
if "$INSERT" --check "$APP/$BIN_REL" "$DYLIB" >/dev/null 2>&1 && [ "$MODE" != "--test" ]; then
  quiet_sparkle
  echo "Rex is already patched"
  exit 0
fi

VERSION=$(defaults read "$APP/Contents/Info.plist" CFBundleVersion 2>/dev/null || echo unknown)
if codesign -dv "$APP" 2>&1 | grep -q "TeamIdentifier=$TEAM"; then
  rm -rf "$PREFIX/backup"
  mkdir -p "$PREFIX/backup"
  ditto "$APP" "$PREFIX/backup/Rex Beta.app"
  echo "$VERSION" > "$PREFIX/backup/VERSION"
  echo "backed up the official Rex $VERSION"
fi

STAGE="$(mktemp -d)"
S="$STAGE/Rex Beta.app"
ditto "$APP" "$S"
if is_fat "$S/$BIN_REL"; then
  lipo -thin "$ARCH" "$S/$BIN_REL" -output "$S/$BIN_REL.thin"
  mv "$S/$BIN_REL.thin" "$S/$BIN_REL"
fi
"$INSERT" "$S/$BIN_REL" "$DYLIB"

ENT="$STAGE/entitlements.plist"
codesign -d --entitlements - --xml "$APP" > "$ENT" 2>/dev/null || true
[ -s "$ENT" ] || printf '<?xml version="1.0" encoding="UTF-8"?>\n<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n<plist version="1.0"><dict/></plist>\n' > "$ENT"
/usr/libexec/PlistBuddy -c "Delete :com.apple.security.cs.disable-library-validation" "$ENT" >/dev/null 2>&1 || true
/usr/libexec/PlistBuddy -c "Add :com.apple.security.cs.disable-library-validation bool true" "$ENT"
codesign -f -s - --options runtime --entitlements "$ENT" "$S" 2>/dev/null
codesign --verify --strict "$S"

if [ "$MODE" = "--test" ]; then
  rm -rf "$PREFIX/test"
  mkdir -p "$PREFIX/test"
  mv "$S" "$PREFIX/test/Rex Beta.app"
  echo "test copy: $PREFIX/test/Rex Beta.app"
  exit 0
fi

# Keep the replaced bundle until the next run: a running Rex still uses it.
rm -rf "$PREFIX/replaced"
mkdir -p "$PREFIX/replaced"
mv "$APP" "$PREFIX/replaced/"
mv "$S" "$APP"
quiet_sparkle
echo "patched Rex $VERSION"
