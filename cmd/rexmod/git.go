package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

func git(dir string, args ...string) (string, error) {
	return gitEnv(dir, nil, args...)
}

func gitEnv(dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Env = append(cmd.Env, "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func repoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// stateDir is where rexmod keeps per-worktree state: comments and turn
// snapshots. It lives inside the git dir so it never shows up in the repo.
func stateDir(repo string) (string, error) {
	out, err := git(repo, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return "", err
	}
	d := filepath.Join(strings.TrimSpace(out), "rexmod")
	return d, os.MkdirAll(d, 0o755)
}

func currentBranch(repo string) string {
	out, err := git(repo, "symbolic-ref", "--short", "-q", "HEAD")
	if err == nil && strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	out, err = git(repo, "rev-parse", "--short", "HEAD")
	if err == nil {
		return strings.TrimSpace(out)
	}
	return ""
}

func baseName(p string) string { return filepath.Base(p) }

func headTree(repo string) string {
	out, err := git(repo, "rev-parse", "-q", "--verify", "HEAD^{tree}")
	if err != nil {
		return emptyTree
	}
	return strings.TrimSpace(out)
}

// snapshotTree writes the current working tree, including untracked files
// that are not ignored, as a git tree object and returns its id. It uses a
// throwaway index so the real index, stash and refs stay untouched.
func snapshotTree(repo string) (string, error) {
	idxPath, err := git(repo, "rev-parse", "--git-path", "index")
	if err != nil {
		return "", err
	}
	idxPath = strings.TrimSpace(idxPath)
	if !filepath.IsAbs(idxPath) {
		idxPath = filepath.Join(repo, idxPath)
	}
	tmp, err := os.CreateTemp("", "rexmod-index-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if src, err := os.Open(idxPath); err == nil {
		_, _ = io.Copy(tmp, src)
		src.Close()
	}
	tmp.Close()
	env := []string{"GIT_INDEX_FILE=" + tmpPath}
	if fi, _ := os.Stat(tmpPath); fi == nil || fi.Size() == 0 {
		os.Remove(tmpPath)
		if _, err := gitEnv(repo, env, "read-tree", "--empty"); err != nil {
			return "", err
		}
	}
	if _, err := gitEnv(repo, env, "add", "-A", "--ignore-errors", "."); err != nil {
		// --ignore-errors still reports unreadable files; the tree is usable.
		if !strings.Contains(err.Error(), "adding files failed") {
			return "", err
		}
	}
	out, err := gitEnv(repo, env, "write-tree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// defaultBase finds the commit the current branch forked from.
func defaultBase(repo string) (base string, label string) {
	var candidates []string
	if out, err := git(repo, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if s := strings.TrimSpace(out); s != "" {
			candidates = append(candidates, s)
		}
	}
	candidates = append(candidates, "origin/main", "origin/master", "main", "master")
	branch := currentBranch(repo)
	for _, c := range candidates {
		if _, err := git(repo, "rev-parse", "-q", "--verify", c+"^{commit}"); err != nil {
			continue
		}
		// On the default branch itself, compare against its remote.
		if c == branch {
			continue
		}
		mb, err := git(repo, "merge-base", "HEAD", c)
		if err != nil {
			continue
		}
		return strings.TrimSpace(mb), c
	}
	if up, err := git(repo, "rev-parse", "-q", "--verify", "@{upstream}"); err == nil {
		mb, err := git(repo, "merge-base", "HEAD", strings.TrimSpace(up))
		if err == nil {
			return strings.TrimSpace(mb), "upstream"
		}
	}
	return "HEAD", "HEAD"
}

type FileChange struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Status  string `json:"status"` // A, M, D, R, T
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	Binary  bool   `json:"binary,omitempty"`
	Staged  bool   `json:"staged,omitempty"` // uncommitted mode: all of it is in the index
}

type Changes struct {
	Mode    string       `json:"mode"`
	Base    string       `json:"base,omitempty"`
	BaseRef string       `json:"base_ref,omitempty"`
	Running bool         `json:"running,omitempty"`
	Since   string       `json:"since,omitempty"`
	Note    string       `json:"note,omitempty"`
	Files   []FileChange `json:"files"`
	Add     int          `json:"add"`
	Del     int          `json:"del"`
}

func diffTrees(repo, from, to string) ([]FileChange, error) {
	ns, err := git(repo, "diff", "--no-ext-diff", "--no-color", "-M", "-z", "--name-status", from, to)
	if err != nil {
		return nil, err
	}
	byPath := map[string]*FileChange{}
	var order []string
	parts := strings.Split(ns, "\x00")
	for i := 0; i < len(parts); i++ {
		st := parts[i]
		if st == "" {
			continue
		}
		fc := &FileChange{Status: st[:1]}
		if fc.Status == "R" || fc.Status == "C" {
			if i+2 >= len(parts) {
				break
			}
			fc.OldPath, fc.Path = parts[i+1], parts[i+2]
			i += 2
			if fc.Status == "C" {
				fc.Status = "A"
			}
		} else {
			if i+1 >= len(parts) {
				break
			}
			fc.Path = parts[i+1]
			i++
		}
		byPath[fc.Path] = fc
		order = append(order, fc.Path)
	}
	num, err := git(repo, "diff", "--no-ext-diff", "--no-color", "-M", "-z", "--numstat", from, to)
	if err == nil {
		parts := strings.Split(num, "\x00")
		for i := 0; i < len(parts); i++ {
			rec := parts[i]
			if rec == "" {
				continue
			}
			f := strings.SplitN(rec, "\t", 3)
			if len(f) < 3 {
				continue
			}
			path := f[2]
			if path == "" { // rename: old and new follow
				if i+2 >= len(parts) {
					break
				}
				path = parts[i+2]
				i += 2
			}
			fc := byPath[path]
			if fc == nil {
				continue
			}
			if f[0] == "-" {
				fc.Binary = true
				continue
			}
			fc.Add, _ = strconv.Atoi(f[0])
			fc.Del, _ = strconv.Atoi(f[1])
		}
	}
	out := make([]FileChange, 0, len(order))
	for _, p := range order {
		out = append(out, *byPath[p])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// computeChanges diffs the working tree (or tree now) against the base of
// mode. For last-turn, block picks the agent whose turn it is.
func computeChanges(repo, mode, now, block string) (*Changes, error) {
	var err error
	if now == "" {
		now, err = snapshotTree(repo)
		if err != nil {
			return nil, err
		}
	}
	c := &Changes{Mode: mode, Files: []FileChange{}}
	var from string
	switch mode {
	case "uncommitted":
		from = headTree(repo)
		c.Base, c.BaseRef = from, "HEAD"
	case "branch":
		base, label := defaultBase(repo)
		from = base
		c.Base, c.BaseRef = base, label
	case "last-turn", "turn":
		c.Mode = "last-turn"
		t := latestTurn(repo, block)
		if t == nil {
			c.Note = "No turn from this agent recorded in this repo yet."
			return c, nil
		}
		from = t.Start
		c.Base = t.Start
		c.Since = t.StartedAt
		if t.End != "" {
			now = t.End
		} else {
			c.Running = true
		}
	default:
		return nil, fmt.Errorf("unknown mode %q (last-turn, uncommitted, branch)", mode)
	}
	files, err := diffTrees(repo, from, now)
	if err != nil {
		return nil, err
	}
	c.Files = files
	if c.Mode == "uncommitted" {
		markStaged(repo, c.Files)
	}
	for _, f := range files {
		c.Add += f.Add
		c.Del += f.Del
	}
	return c, nil
}

// markStaged flags the files whose whole change is in the index. A file with
// more edits on top of what's staged stays unflagged: a file is listed once,
// and its numbers cover the edits that aren't staged yet.
func markStaged(repo string, files []FileChange) {
	staged, err := git(repo, "diff", "--no-ext-diff", "--cached", "-M", "-z", "--name-only")
	if err != nil {
		return
	}
	dirty, err := git(repo, "diff", "--no-ext-diff", "-z", "--name-only")
	if err != nil {
		return
	}
	notStaged := map[string]bool{}
	for _, p := range strings.Split(dirty, "\x00") {
		notStaged[p] = true
	}
	inIndex := map[string]bool{}
	for _, p := range strings.Split(staged, "\x00") {
		inIndex[p] = true
	}
	for i := range files {
		files[i].Staged = inIndex[files[i].Path] && !notStaged[files[i].Path]
	}
}

// listFiles returns tracked, untracked and loose ignored files, relative to repo.
func listFiles(repo string, changedOnly bool) ([]string, error) {
	if changedOnly {
		c, err := computeChanges(repo, "branch", "", "")
		if err != nil {
			return nil, err
		}
		var out []string
		for _, f := range c.Files {
			if f.Status != "D" {
				out = append(out, f.Path)
			}
		}
		return out, nil
	}
	out, err := git(repo, "ls-files", "-z", "-c", "-o", "--exclude-standard", "--deduplicate")
	if err != nil {
		return nil, err
	}
	// Ignored files that sit on their own, such as .env.development.local;
	// a wholly ignored directory (node_modules, build output) stays out.
	if ign, err := git(repo, "ls-files", "-z", "-o", "-i", "--exclude-standard", "--directory", "--no-empty-directory"); err == nil {
		for _, f := range strings.Split(ign, "\x00") {
			if f != "" && !strings.HasSuffix(f, "/") {
				out += "\x00" + f
			}
		}
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f == "" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(repo, f)); err != nil {
			continue // deleted in the working tree
		}
		files = append(files, f)
	}
	return files, nil
}

// plainIgnoreDirs lists directories skipped when a folder has no git metadata
// to read ignore rules from. Without it, one node_modules or build output
// would drown the tree.
var plainIgnoreDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true,
	"node_modules": true, "bower_components": true, "vendor": true,
	"__pycache__": true, ".tox": true, ".mypy_cache": true, ".pytest_cache": true,
	"dist": true, "build": true, "out": true, "target": true,
	".next": true, ".nuxt": true, ".cache": true, ".gradle": true, ".build": true,
	"Pods": true, "DerivedData": true,
}

// listPlainFiles walks a folder that isn't a git repository and returns every
// file, relative to dir. Hidden directories (and so the ignore list above) are
// pruned; hidden files such as .env stay, like the loose ignored files of a
// git repository. Symlinks are listed but never followed.
func listPlainFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable or vanished: skip, don't abort the walk
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") || plainIgnoreDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// listRepoFiles lists the files the Files tab shows: git-tracked, untracked
// and loose ignored files for a repository, or a plain walk of the folder
// when it isn't a git repository.
func listRepoFiles(dir string, changedOnly bool) ([]string, error) {
	if _, err := repoRoot(dir); err == nil {
		return listFiles(dir, changedOnly)
	}
	if changedOnly {
		return []string{}, nil // no git, no branch to diff against
	}
	return listPlainFiles(dir)
}

// Turn is one agent turn: the working tree when the prompt was submitted and
// when the agent stopped.
type Turn struct {
	SessionID string `json:"session_id"`
	Block     string `json:"block,omitempty"` // Rex pane of the agent (REX_BLOCK)
	Start     string `json:"start"`
	StartedAt string `json:"started_at"`
	End       string `json:"end,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
	// Prompt keys the user message that started the turn (promptKey), for
	// agents whose prompt hook can fire again within it (agy).
	Prompt string `json:"prompt,omitempty"`
}

func turnsDir(repo string) (string, error) {
	sd, err := stateDir(repo)
	if err != nil {
		return "", err
	}
	d := filepath.Join(sd, "turns")
	return d, os.MkdirAll(d, 0o755)
}

func sanitize(s string) string {
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == '.' {
			return '_'
		}
		return r
	}, s)
}

func readTurn(path string) *Turn {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var t Turn
	if json.Unmarshal(b, &t) != nil || t.Start == "" {
		return nil
	}
	return &t
}

func writeJSONAtomic(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// latestTurn returns the newest turn of the agent in Rex pane block. Turns
// recorded outside Rex (no pane) stand in when that agent has none; another
// agent's turn never does. An empty block means any agent.
func latestTurn(repo, block string) *Turn {
	d, err := turnsDir(repo)
	if err != nil {
		return nil
	}
	entries, _ := os.ReadDir(d)
	var best, loose *Turn
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		t := readTurn(filepath.Join(d, e.Name()))
		if t == nil {
			continue
		}
		switch {
		case block == "" || sameBlock(t.Block, block):
			if best == nil || t.StartedAt > best.StartedAt {
				best = t
			}
		case t.Block == "":
			if loose == nil || t.StartedAt > loose.StartedAt {
				loose = t
			}
		}
	}
	if best == nil {
		return loose
	}
	return best
}

// sameBlock compares Rex block IDs given in full or by suffix.
func sameBlock(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasSuffix(a, b) || strings.HasSuffix(b, a)
}

// turnFile names the turn record of an agent session, or of its pane when
// the agent gives no session ID.
func turnFile(d, session string) string {
	if session == "" {
		session = "pane-" + os.Getenv("REX_BLOCK")
	}
	return filepath.Join(d, sanitize(session)+".json")
}

func turnStart(repo, session, prompt string) error {
	tree, err := snapshotTree(repo)
	if err != nil {
		return err
	}
	d, err := turnsDir(repo)
	if err != nil {
		return err
	}
	t := Turn{SessionID: session, Block: os.Getenv("REX_BLOCK"), Start: tree, StartedAt: time.Now().Format(time.RFC3339Nano), Prompt: prompt}
	return writeJSONAtomic(turnFile(d, session), t)
}

func turnStop(repo, session string) error {
	d, err := turnsDir(repo)
	if err != nil {
		return err
	}
	path := turnFile(d, session)
	t := readTurn(path)
	if t == nil {
		return nil
	}
	tree, err := snapshotTree(repo)
	if err != nil {
		return err
	}
	t.End = tree
	t.EndedAt = time.Now().Format(time.RFC3339Nano)
	return writeJSONAtomic(path, t)
}
