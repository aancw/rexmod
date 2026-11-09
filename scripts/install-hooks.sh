#!/bin/sh
# Hooks rexmod into each coding agent Rex supports that is installed here:
#
#   Claude Code  ~/.claude/settings.json            UserPromptSubmit + Stop command hooks
#   Codex        ~/.codex/hooks.json                the same hooks (Codex asks you to trust
#                                                   new hooks once: run /hooks in Codex)
#   OpenCode     ~/.config/opencode/plugins/rexmod.js   server plugin (chat.message, session.idle)
#                ~/.config/opencode/plugins/rexmod-tui/   TUI plugin, listed in cli.json/tui.json: tells
#                                                   rexmod which Rex pane shows a session
#   Pi           ~/.pi/agent/extensions/rexmod.ts       extension (before_agent_start, agent_settled)
#   Antigravity  ~/.gemini/config/plugins/rexmod/      plugin: PreInvocation + Stop command hooks,
#                                                   and the skill
#
# Each agent also gets the rexmod skill (skills/rexmod): Claude Code in
# ~/.claude/skills/rexmod, Antigravity in its plugin, the others in
# ~/.agents/skills/rexmod.
#
# On each message you send, `rexmod hook prompt` snapshots the working tree and
# hands the agent the review comments you left for it in the editor; when the
# agent is done, `rexmod hook stop` snapshots again. That pair is "Last Turn".
#
#   install-hooks.sh            add (safe to run again; JSON files are backed up first)
#   install-hooks.sh --remove   take them out again
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
AGENTS="$HERE/../agents"
BIN="$HOME/.local/bin/rexmod"
MODE=add
[ "${1:-}" = "--remove" ] && MODE=remove

CLAUDE_DIR="${CLAUDE_CONFIG_DIR:-$HOME/.claude}"
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
OPENCODE_DIR="${OPENCODE_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/opencode}"
PI_DIR="${PI_CODING_AGENT_DIR:-$HOME/.pi/agent}"
AGY_DIR="$HOME/.gemini/config"

has() { command -v "$1" >/dev/null 2>&1 || [ -d "$2" ]; }

# hooks_json FILE [allow]: add or remove the two command hooks in a Claude Code style
# hooks file (Claude's settings.json and Codex's hooks.json share the format).
# With allow (Claude), also allow `rexmod comments resolve`: the agent runs it
# to clear a note it addressed, and auto mode blocks it otherwise.
hooks_json() {
  file="$1"
  if [ "$MODE" = remove ] && [ ! -f "$file" ]; then return 0; fi
  mkdir -p "$(dirname "$file")"
  [ -f "$file" ] || echo '{}' > "$file"
  cp "$file" "$file.bak-rexmod"
  /usr/bin/python3 - "$file" "$BIN" "$MODE" "${2:-}" <<'EOF'
import json, sys
path, binary, mode, allow = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4] == "allow"
raw = open(path).read()
d = json.loads(raw or "{}")
indent = 4 if raw.startswith('{\n    "') else 2
hooks = d.setdefault("hooks", {})
mine = lambda h: "rexmod hook" in h.get("command", "")
changed = False
if mode == "remove":
    for event in list(hooks):
        groups = []
        for g in hooks[event]:
            kept = [h for h in g.get("hooks", []) if not mine(h)]
            changed |= len(kept) != len(g.get("hooks", []))
            if kept:
                g["hooks"] = kept
                groups.append(g)
        if groups:
            hooks[event] = groups
        else:
            del hooks[event]
    if not hooks:
        del d["hooks"]
else:
    for event, arg in (("UserPromptSubmit", "prompt"), ("Stop", "stop")):
        groups = hooks.setdefault(event, [])
        if any(mine(h) for g in groups for h in g.get("hooks", [])):
            continue
        groups.append({"hooks": [{"type": "command", "command": f"{binary} hook {arg}", "timeout": 15}]})
        changed = True
if allow:
    rule = "Bash(rexmod comments resolve:*)"
    perms = d.setdefault("permissions", {})
    rules = perms.setdefault("allow", [])
    if mode == "remove" and rule in rules:
        rules.remove(rule)
        changed = True
    elif mode != "remove" and rule not in rules:
        rules.append(rule)
        changed = True
    if not rules:
        del perms["allow"]
    if not perms:
        del d["permissions"]
open(path, "w").write(json.dumps(d, indent=indent, ensure_ascii=False) + "\n")
print(("removed from" if mode == "remove" else "added to") if changed else "unchanged:", path)
EOF
}

# plugin SRC DEST: install a plugin file with the rexmod path filled in.
plugin() {
  if [ "$MODE" = remove ]; then
    [ -f "$2" ] && rm -f "$2" && echo "removed $2"
    return 0
  fi
  mkdir -p "$(dirname "$2")"
  sed "s#__REXMOD__#$BIN#" "$1" > "$2.tmp" && mv -f "$2.tmp" "$2"
  echo "installed $2"
}

# skill DIR: install (or remove) the rexmod skill into an agent's skills dir.
skill() {
  if [ "$MODE" = remove ]; then
    [ -f "$1/rexmod/SKILL.md" ] && rm -rf "$1/rexmod" && echo "removed $1/rexmod"
    return 0
  fi
  mkdir -p "$1/rexmod"
  cp "$HERE/../skills/rexmod/SKILL.md" "$1/rexmod/SKILL.md"
  echo "installed $1/rexmod"
}

# agy_plugin DIR: install (or remove) the Antigravity plugin, which rexmod
# owns whole: its hooks and the skill.
agy_plugin() {
  if [ "$MODE" = remove ]; then
    grep -qs '"name": "rexmod"' "$1/plugin.json" && rm -rf "$1" && echo "removed $1"
    return 0
  fi
  mkdir -p "$1/skills/rexmod"
  cp "$AGENTS/antigravity/plugin.json" "$1/plugin.json"
  sed "s#__REXMOD__#$BIN#" "$AGENTS/antigravity/hooks.json" > "$1/hooks.json"
  cp "$HERE/../skills/rexmod/SKILL.md" "$1/skills/rexmod/SKILL.md"
  echo "installed $1"
}

# tui_list FILE KEY ENTRY: list (or unlist) a TUI plugin in an OpenCode config
# file: tui.json "plugin" (OpenCode 1) or cli.json "plugins" (OpenCode 2).
tui_list() {
  if [ "$MODE" = remove ] && [ ! -f "$1" ]; then return 0; fi
  [ -f "$1" ] || echo '{}' > "$1"
  /usr/bin/python3 - "$1" "$2" "$3" "$MODE" <<'PY'
import json, sys
path, key, entry, mode = sys.argv[1:5]
d = json.loads(open(path).read() or "{}")
plugins = [p for p in d.get(key, []) if "rexmod-tui" not in (p if isinstance(p, str) else json.dumps(p))]
if mode != "remove":
    plugins.append(entry)
if plugins:
    d[key] = plugins
else:
    d.pop(key, None)
if path.endswith("cli.json") and d and "$schema" not in d:
    d = {"$schema": "https://opencode.ai/v2/cli.json", **d}
open(path, "w").write(json.dumps(d, indent=2) + "\n")
PY
}

found=0
if has claude "$CLAUDE_DIR"; then found=1; hooks_json "$CLAUDE_DIR/settings.json" allow; fi
if has codex "$CODEX_DIR"; then
  found=1
  hooks_json "$CODEX_DIR/hooks.json"
  [ "$MODE" = add ] && echo "    Codex: start it and run /hooks once to trust the two rexmod hooks."
fi
if has opencode "$OPENCODE_DIR"; then
  found=1
  plugin "$AGENTS/opencode-rexmod.js" "$OPENCODE_DIR/plugins/rexmod.js"
  plugin "$AGENTS/opencode-rexmod-tui.js" "$OPENCODE_DIR/plugins/rexmod-tui/tui.js"
  [ "$MODE" = remove ] && rmdir "$OPENCODE_DIR/plugins/rexmod-tui" 2>/dev/null || true
  tui_list "$OPENCODE_DIR/tui.json" plugin "file://$OPENCODE_DIR/plugins/rexmod-tui/tui.js"
  tui_list "$OPENCODE_DIR/cli.json" plugins "file://$OPENCODE_DIR/plugins/rexmod-tui/tui.js"
  # OpenCode 2 keeps one background server; it loads new plugins when it restarts.
  [ "$MODE" = add ] && echo "    OpenCode: quit OpenCode (or run: pkill -f 'opencode serve') so it loads the plugin."
fi
if has pi "$PI_DIR"; then found=1; plugin "$AGENTS/pi-rexmod.ts" "$PI_DIR/extensions/rexmod.ts"; fi
if has agy "$AGY_DIR"; then
  found=1
  agy_plugin "$AGY_DIR/plugins/rexmod"
  [ "$MODE" = add ] && echo "    Antigravity: restart agy so it loads the plugin."
fi
if has claude "$CLAUDE_DIR"; then skill "$CLAUDE_DIR/skills"; fi
if has codex "$CODEX_DIR" || has opencode "$OPENCODE_DIR" || has pi "$PI_DIR"; then
  skill "$HOME/.agents/skills" # read by Codex, OpenCode and Pi
fi
[ "$found" = 1 ] || echo "no supported agent found (Claude Code, Codex, OpenCode, Pi, Antigravity)"
