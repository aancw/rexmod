# Changelog

All notable changes to rexmod. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [0.1.13] - 2026-11-03

### Fixed

- The panel follows an agent into a worktree. Claude Code's worktrees (`.claude/worktrees/<name>`) move only the session, not the agent's process, so the panel kept showing the main checkout and its branch. Each agent's hooks now record where it really works, for its pane: Claude Code, Codex, OpenCode and Pi. The record holds while the same agent runs in the pane.
- Codex and OpenCode notes reach the right pane when the agent works in another worktree of the repository than the one its pane was opened in.

## [0.1.12] - 2026-11-03

### Changed

- The Changes panel is laid out as three tabs, each with its own actions: *Info*, *Changes* and *Files*. A tab shows only its icon until you select it, so they fit a narrow panel, and the Changes tab shows how many comments are pending. Find File and Refresh moved from the header to the tabs they act on.
- The *Changes* tab leads with the branch and its added and removed lines, then lists files with the status first, in groups: *Staged* and *Unstaged* for Uncommitted, *Changed* for Last Turn and Branch. The tree is one click away and stays selected.
- Pending review comments moved from a popover to the top of the *Changes* tab, with a *Send* button. The comment bubble in the header is gone.
- The panel opens and closes with the spring Rex's own sidebar uses (measured frame by frame), and stays on screen while Rex's content slides back over it instead of vanishing first.
- Motion: switching tabs widens the new tab in place as its surface fades in and narrows the old one as its surface fades out, in 150 ms, then swaps the icon colours and fades in the tab's name; the selection moves between Last Turn, Uncommitted and Branch the same way; folder chevrons turn and their files fade in; files the agent touches, and comments once sent, fade in and out; counts roll; the "in progress" dot breathes. Nothing moves with Reduce Motion on.

### Added

- The editor pane runs one headless Neovim behind Rex panes, each Neovim window in its own Rex pane (`rexmod editor`). Ported from neorex (MIT). The bundled Neovim config draws images, statuslines and floats correctly in it.
- The panel follows Rex's Interface settings: selected tabs, pills and buttons are drawn the way Rex draws its selected tab in Classic and in Glass, tinted with the theme or not, light or dark, and flat in an inactive window. A change in Rex's Settings shows at once.
- An *Info* tab: the working directory with Copy Path and Reveal in Finder; the followed agent's state as it reports it to Rex (working, waiting for you, idle), with Copy Session ID and Copy Resume Command for Claude Code; your Claude plan's session (5h) and weekly (7d) usage with their reset times, read from what Claude Code caches; the jobs running in the agent's pane; the ports the tab's panes listen on (click one to open it); the other agents of the tab (click one to follow it); and the pending comments.
- `rexmod info` prints the same as JSON.
- `rexmod state` and `rexmod changes` mark files as `staged` in the uncommitted view when all of their change is in the index.

### Fixed

- "Uncommitted" no longer gets cut short in the panel's default width.
- The first file tree no longer stalls while the icon theme loads; it loads in the background at start.
- The panel no longer draws over Rex's content while it opens or closes: it is revealed and covered by Rex's moving edge, as Rex's sidebar is.
- Opening and closing the panel does less work per frame: the panel is revealed by a mask instead of being moved, the toggle isn't placed again on every frame, and the panel isn't redrawn when it starts. The terminal no longer goes past its final width with the spring's overshoot, which made it rewrap (and an agent redraw) twice more as the panel settled.
- A button's shortcut tip and hover circle no longer stay on screen when the button is hidden or moves away from the pointer (for instance the panel toggle, after `⌘⇧E`).

## [0.1.11] - 2026-11-02

### Fixed

- Review comments no longer disappear from the Changes panel when the agent pane they were for closes. They show for the agent the panel follows and go when you send them from the panel; they never ride along with an agent's next message.
- Comments an agent adds (a review you asked it for) are for another agent in the tab, not for the agent that wrote them, so they reach the agent that asked for the review.

## [0.1.10] - 2026-11-02

### Fixed

- Setup allows `rexmod comments resolve` in Claude Code (`Bash(rexmod comments resolve:*)` in `~/.claude/settings.json`), so the agent can clear the comments it addressed in auto mode without you adding the rule. Uninstalling removes it.

### Security

- `rexmod comments resolve` only clears a comment already sent to the agent that runs it. Code quoted in a comment can't get the agent to clear comments still waiting to be sent or meant for another agent.

## [0.1.9] - 2026-11-02

### Added

- The agent clears the review comments it addressed. Each comment it gets carries its id, and once it has acted on one it runs `rexmod comments resolve --id <id>`, which removes the comment from your editor. Comments it didn't act on (a question, say) stay where they are. In Claude Code's auto mode, allow the command first: add `"Bash(rexmod comments resolve:*)"` to `permissions.allow` in `~/.claude/settings.json` (0.1.10's setup does it for you).

### Fixed

- Neovim redraws review comments as soon as they change outside the editor (sent to the agent, resolved), instead of waiting for you to switch back to it.

## [0.1.8] - 2026-11-02

### Added

- The Changes panel shows where your pending review comments are: each file in the tree shows how many it has, and a closed folder shows the total of the files inside it. The comment bubble in the header now opens a list of them instead of sending them right away. Click a comment to open the editor at its line; the Send button at the bottom sends them all to the agent.
- `rexmod comments add --hold` adds a comment that doesn't go along with the agent's next message. It waits in the panel until you send it, so comments from an automated review don't reach the agent half-finished.

## [0.1.7] - 2026-11-02

### Fixed

- The Changes panel follows the Rex theme: icon buttons, the shortcut tip and the header icons take their colors from the active theme, so a light theme no longer gets a dark tip or white-on-white icons. Status colors get a darker variant on light themes, and the panel refreshes when the appearance or Rex's settings change.
- The `⌘P` picker follows Rex's appearance; its colors and preview were always dark.

## [0.1.6] - 2026-11-01

### Added

- Review comments check the code before they go to the agent. If the lines you reviewed moved, the comment follows them to their new place. If the code changed after your review, the agent gets both the code you reviewed and what's there now, with a note to check the comment still applies before acting on it. A comment also keeps the three lines above and below it, so a change right next to it (a guard added above the commented line, say) is flagged the same way.

### Security

- Review comments pasted into the agent's pane no longer carry control characters from the file, so code containing an escape sequence (such as the end of a bracketed paste) can't type into the agent. Code blocks are fenced with more backticks than the code itself contains.

## [0.1.5] - 2026-11-01

### Added

- Media previews in the editor: images and PDFs open as pictures, videos show their first frame (`Space u o` plays them in the macOS app), and markdown renders in place (`Space c p` opens a live browser preview).

### Fixed

- Rex's own "Check for Updates…" now runs rexmod's `update.sh` (official build, checked, then patched) and reports the result, and Rex's "Checking for updates" status clears afterwards, instead of starting Sparkle and ending in "Update failed".
- Images in the editor pane stayed black: Neovim 0.12 runs its screen in a separate process, and the image plugin's `nvim_ui_send` writes never reached Rex. In Rex they now go straight to the pane's terminal.
- An image stayed on screen after you switched to another buffer; it's now removed when its buffer leaves the window.

## [0.1.4] - 2026-11-01

### Fixed

- The review comment box no longer pops up code completion while you type a note.
- Rex updates: the patched Rex showed "Update failed" because Sparkle's installer only accepts an app signed by Superlogical. Sparkle's checks are now off while Rex is patched (and back on after `unpatch.sh`), and the re-patch agent installs new Rex versions itself every hour with `update.sh`.

## [0.1.3] - 2026-11-01

### Fixed

- Codex: turns and comments go to the right pane. Codex runs hooks in a shared background server whose environment names whichever pane started it; rexmod now finds the pane where Codex works in that folder.
- OpenCode 2: the plugins load again (OpenCode 2 wants `{ id, setup }`), comments join your message through its `prompt` hook, a turn ends on `session.execution.*`, and the TUI half is listed in `cli.json`.

## [0.1.2] - 2026-11-01

### Fixed

- The installer installs Node when it's missing: without it, language servers installed with npm (TypeScript, JSON, YAML, CSS, Pyright…) couldn't be set up on a fresh Mac.

## [0.1.1] - 2026-11-01

### Added

- Homebrew: `brew install alpcanaydin/rexmod/rexmod`, then `rexmod-setup`.

### Fixed

- Only one patch runs at a time. After Rex updates itself, a re-patch agent and an install (or two agents) could swap the app bundle at once.

### Changed

- The README tells rexmod as what it is, a harness around your agent, and plays the demo video in place.

## [0.1.0] - 2026-11-01

First release.

### Added

- Changes panel on the right of the Rex window: Last Turn, Uncommitted and Branch change sets, and a Files tab, styled like Rex's own sidebar. `⌘⇧E` shows or hides it; the toggle sits with the pane actions.
- Editor pane: click a file and it opens in Neovim next to your agent, at the changed line; one editor per tab.
- Review comments: `<Space>r` on a line or selection in the editor; they reach the agent with your next message, or right away from the panel.
- `⌘P` file picker over the agent's repository, with the editor's syntax highlighting in the preview.
- Works with every agent Rex supports: Claude Code and Codex (hooks), OpenCode (plugin), Pi (extension). With several agents in a tab the panel follows the one you focused last.
- Language support on demand in the editor: open a file and its tree-sitter parser and language server are installed and started.
- Installer for a clean Mac, an uninstaller, and a LaunchAgent that re-applies the patch after Rex updates itself.
