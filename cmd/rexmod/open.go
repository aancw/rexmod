package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func cacheDir() string {
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	d = filepath.Join(d, "rexmod")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func shortID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if i := strings.LastIndex(id, ":"); i >= 0 {
		id = id[i+1:]
	}
	if len(id) > 12 {
		id = id[len(id)-12:]
	}
	return id
}

// editorSocket is the Neovim server address for the editor pane of a window.
func editorSocket(windowID string) string {
	return filepath.Join(cacheDir(), "nvim-"+shortID(windowID)+".sock")
}

func vimStr(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func nvimBin() string {
	if p, err := exec.LookPath("nvim"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/nvim", "/usr/local/bin/nvim"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "nvim"
}

func nvimAlive(sock string) bool {
	if _, err := os.Stat(sock); err != nil {
		return false
	}
	out, err := exec.Command(nvimBin(), "--server", sock, "--remote-expr", "1").Output()
	return err == nil && strings.TrimSpace(string(out)) == "1"
}

func nvimRemoteOpen(sock, abs string, line int) error {
	lua := "(function(p,l) local ok,m=pcall(require,'rexmod') if ok then m.open(p,l) else vim.cmd('drop '..vim.fn.fnameescape(p)) if l and l>0 then pcall(vim.api.nvim_win_set_cursor,0,{l,0}) end end return 1 end)(_A[1],_A[2])"
	expr := fmt.Sprintf(`luaeval("%s", [%s, %d])`, lua, vimStr(abs), line)
	out, err := exec.Command(nvimBin(), "--server", sock, "--remote-expr", expr).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nvim remote: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// openInEditor opens file at line in the window's editor pane, creating the
// pane next to the agent pane when there is none.
func openInEditor(session, window, file string, line int) error {
	t, err := resolveTarget(session, window)
	if err != nil {
		return err
	}
	abs := file
	if !filepath.IsAbs(abs) {
		base := t.Repo
		if base == "" {
			base = t.Cwd
		}
		if base == "" {
			base, _ = os.Getwd()
		}
		abs = filepath.Join(base, file)
	}
	sock := editorSocket(t.WindowID)

	if t.EditorBlockID != "" && nvimAlive(sock) {
		if err := nvimRemoteOpen(sock, abs, line); err != nil {
			return err
		}
		_, err := rexRun("focus", "-s", t.SessionID, t.EditorBlockID)
		return err
	}
	if t.EditorBlockID != "" {
		// A pane is labelled editor but its Neovim is gone; replace it.
		_, _ = rexRun("block", "close", "-s", t.SessionID, t.EditorBlockID)
	}
	_ = os.Remove(sock)

	anchor := t.AnchorBlockID
	cwd := t.Repo
	if cwd == "" {
		cwd = filepath.Dir(abs)
	}
	args := []string{"split", "-s", t.SessionID, "--split", "right", "--label", editorLabel, "--cwd", cwd, "--shell", "none"}
	if anchor != "" {
		args = append(args, "-b", anchor)
	}
	extra := nvimEnv()
	extra["REXMOD_EDITOR"] = "1"
	extra["REXMOD_SESSION"] = t.SessionID
	extra["REXMOD_WINDOW"] = t.WindowID
	env := envArgs(extra)
	args = append(args, "--", "/usr/bin/env")
	args = append(args, env...)
	// rexmod's editor: one Neovim behind Rex panes, each window its own pane
	args = append(args, selfPath(), "editor", "--listen", sock)
	if line > 0 {
		args = append(args, fmt.Sprintf("+%d", line))
	}
	args = append(args, abs)
	_, err = rexRun(args...)
	return err
}

// parseFileLine splits "path:12" into path and line.
func parseFileLine(s string) (string, int) {
	if i := strings.LastIndex(s, ":"); i > 0 {
		var n int
		if _, err := fmt.Sscanf(s[i+1:], "%d", &n); err == nil && n > 0 {
			if _, err := os.Stat(s); err != nil { // not a file literally named "x:12"
				return s[:i], n
			}
		}
	}
	return s, 0
}
