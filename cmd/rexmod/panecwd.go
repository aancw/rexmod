package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// An agent can move into another directory without its process moving:
// Claude Code's worktrees (.claude/worktrees/<name>) change only the
// session's own working directory. The hooks know the real one, so they
// record it per pane; resolveTarget prefers it over the process cwd.
//
// The record names the agent's pid (for Claude Code and Pi, the hook's
// ancestors, one of which is the agent), so it holds only while the same
// agent process runs in the pane: a new agent there starts clean.

func paneCwdPath(block string) string {
	return filepath.Join(cacheDir(), "panecwd", sanitize(strings.TrimPrefix(block, "block:")))
}

// hookAncestors returns the pids above the hook: the agent is one of them.
func hookAncestors() []int {
	var pids []int
	for pid, i := os.Getppid(), 0; pid > 1 && i < 6; i++ {
		pids = append(pids, pid)
		pid = parentPid(pid)
	}
	return pids
}

// recordPaneCwd notes that the agent in block, one of pids, works in cwd.
func recordPaneCwd(block, cwd string, pids []int) {
	if block == "" || block == outsideRex || cwd == "" || len(pids) == 0 {
		return
	}
	ids := make([]string, len(pids))
	for i, p := range pids {
		ids[i] = strconv.Itoa(p)
	}
	p := paneCwdPath(block)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, []byte(strings.Join(ids, " ")+"\n"+cwd+"\n"), 0o644) == nil {
		_ = os.Rename(tmp, p)
	}
}

// recordedPaneCwd returns the cwd an agent's hook recorded for block, if
// that agent (pid) is still the one running there and the directory exists.
func recordedPaneCwd(block string, pid int) string {
	b, err := os.ReadFile(paneCwdPath(block))
	if err != nil || pid <= 0 {
		return ""
	}
	pids, cwd, _ := strings.Cut(string(b), "\n")
	cwd = strings.TrimSpace(cwd)
	for _, p := range strings.Fields(pids) {
		if p == strconv.Itoa(pid) {
			if st, err := os.Stat(cwd); err == nil && st.IsDir() {
				return cwd
			}
			return ""
		}
	}
	return ""
}

var parentPid = func(pid int) int {
	out, err := exec.Command("ps", "-o", "ppid=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}

// worktreeInUse returns the worktree of cwd's repository that the agent's
// transcript names last, or cwd. Agents often stay where they started and
// work in a worktree through paths and `cd` (Claude Code without
// EnterWorktree, Codex), so their own cwd doesn't tell.
func worktreeInUse(cwd, transcript string) string {
	if transcript == "" {
		return cwd
	}
	root, err := repoRoot(cwd)
	if err != nil {
		return cwd
	}
	out, err := git(root, "worktree", "list", "--porcelain")
	if err != nil {
		return cwd
	}
	var trees []string
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok && p != root {
			trees = append(trees, p)
		}
	}
	if len(trees) == 0 {
		return cwd
	}
	// Only what the agent itself did: its commands, edits and words. Tool
	// output (git worktree list, ls, grep) names worktrees it isn't in.
	lines := strings.Split(readTail(transcript, 4<<20), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if isToolOutput(l) {
			continue
		}
		best, at := "", -1
		for _, t := range trees {
			if j := lastPathIndex(l, t); j > at {
				best, at = t, j
			}
		}
		if best != "" {
			return best
		}
	}
	return cwd
}

// isToolOutput tells a transcript line that carries a tool's result:
// Claude Code's tool_result, Codex's function_call_output.
func isToolOutput(l string) bool {
	return strings.Contains(l, `"tool_result"`) || strings.Contains(l, `"toolUseResult"`) ||
		strings.Contains(l, `"function_call_output"`) || strings.Contains(l, `"custom_tool_call_output"`)
}

func readTail(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	off := st.Size() - n
	if off < 0 {
		off = 0
	}
	b := make([]byte, st.Size()-off)
	n2, _ := f.ReadAt(b, off)
	return string(b[:n2])
}

// lastPathIndex returns where path last appears in s as a whole path, not
// as the prefix of a longer name (worktrees "a" and "ab"), or -1.
func lastPathIndex(s, path string) int {
	for end := len(s); ; {
		j := strings.LastIndex(s[:end], path)
		if j < 0 {
			return -1
		}
		k := j + len(path)
		if k == len(s) || !isNameByte(s[k]) {
			return j
		}
		end = j
	}
}

func isNameByte(c byte) bool {
	return c == '-' || c == '_' || c == '.' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
