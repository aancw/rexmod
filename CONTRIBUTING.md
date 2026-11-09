# Contributing

Thanks for helping. Issues and pull requests are welcome; for anything big,
open an issue first so we can agree on the shape.

## Setup

You need a Mac with Apple silicon, Rex, Go (see `go.mod`) and Xcode 26 or
later (the panel uses the macOS 26 SDK).

```sh
make build        # rexmod CLI into build/
ln -sf "$PWD/build/rexmod" ~/.local/bin/rexmod
make panel        # panel dylib into ~/Library/Application Support/rexmod; restart Rex
make test lint
make preview      # panel views without Rex: screenshots and checks in build/preview
build/rexmod-preview build/preview --animate   # plays each animation once, to record
```

Point the editor at your checkout so Lua changes are live: in
`~/.config/nvim/lua/plugins/rexmod.lua` set `dir` to `<checkout>/nvim`.

## Where things are

| Path | What |
|---|---|
| `cmd/rexmod` | Go CLI: target resolution, git change sets, editor, picker, comments, hooks |
| `panel/` | Swift dylib Rex loads: the panel and the chrome inset |
| `nvim/` | Neovim plugin (comments, language support on demand) and the picker preview |
| `agents/` | OpenCode plugin, Pi extension and Antigravity plugin |
| `skills/rexmod` | The skill agents get |
| `scripts/` | Patch, update, hooks, re-patch agent, release packaging |

## Panel work

Rex is closed source and its binary is stripped. Before changing layout, read
how Rex does it rather than guessing: the panel has Darwin notifications that
dump views and method addresses (see Debugging in the README), and
`xcrun llvm-objdump -d --start-address=…` on Rex's binary shows the code.
Keep the look identical to Rex's sidebar: colors, hover, selection and sizes
are measured from Rex, not chosen.

## Style

- Go: `gofmt`, small functions, comments that say why.
- Shell: POSIX `sh`, passes `shellcheck -s sh`.
- Commit messages: imperative, short subject line.
