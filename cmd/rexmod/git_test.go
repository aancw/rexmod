package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUncommittedMarksStaged(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	body := "one\ntwo\nthree\nfour\nfive\nsix\n"
	for _, name := range []string{"staged.txt", "unstaged.txt", "partial.txt", "gone.txt", "old.txt"} {
		write(name, name+"\n"+body)
	}
	run("add", "-A")
	run("commit", "-qm", "init")

	write("staged.txt", "changed\n"+body)
	run("add", "staged.txt")
	write("unstaged.txt", "changed\n"+body)
	write("partial.txt", "changed\n"+body)
	run("add", "partial.txt")
	write("partial.txt", "changed again\n"+body)
	run("rm", "-q", "gone.txt")
	run("mv", "old.txt", "new.txt")
	write("added.txt", "added\n")
	run("add", "added.txt")
	write("untracked.txt", "untracked\n")

	c, err := computeChanges(dir, "uncommitted", "", "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"staged.txt": true, "gone.txt": true, "new.txt": true, "added.txt": true,
		"unstaged.txt": false, "partial.txt": false, "untracked.txt": false,
	}
	if len(c.Files) != len(want) {
		t.Fatalf("got %d files: %+v", len(c.Files), c.Files)
	}
	for _, f := range c.Files {
		staged, ok := want[f.Path]
		if !ok {
			t.Errorf("unexpected file %q", f.Path)
		} else if f.Staged != staged {
			t.Errorf("%s: staged = %v, want %v", f.Path, f.Staged, staged)
		}
	}

	// Only the uncommitted view splits by index: the other modes leave it off.
	b, err := computeChanges(dir, "branch", "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range b.Files {
		if f.Staged {
			t.Errorf("branch mode flagged %s", f.Path)
		}
	}
}

func TestListPlainFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", "a\n")
	write(".env", "SECRET=1\n")                 // hidden file: kept
	write("src/main.go", "package main\n")      // nested: kept
	write("src/util/util.go", "package util\n") // nested: kept
	write(".hidden/inside.txt", "x\n")          // hidden dir: pruned
	write("node_modules/pkg/index.js", "x\n")   // ignored dir: pruned
	write("build/output.bin", "x\n")            // ignored dir: pruned
	if err := os.Symlink("a.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	files, err := listPlainFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	want := []string{".env", "a.txt", "link.txt", "src/main.go", "src/util/util.go"}
	if len(files) != len(want) {
		t.Fatalf("got %v, want %v", files, want)
	}
	for _, f := range want {
		if !got[f] {
			t.Errorf("missing %q in %v", f, files)
		}
	}
}

func TestListRepoFilesDispatch(t *testing.T) {
	// A plain folder walks the directory; a git repository lists git files.
	plain := t.TempDir()
	if err := os.WriteFile(filepath.Join(plain, "note.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := listRepoFiles(plain, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != "note.txt" {
		t.Fatalf("plain folder: got %v, want [note.txt]", files)
	}
	if changed, err := listRepoFiles(plain, true); err != nil || len(changed) != 0 {
		t.Fatalf("plain folder changed-only: got %v, %v", changed, err)
	}

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "committed.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err = listRepoFiles(repo, false)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f] = true
	}
	for _, f := range []string{"committed.txt", "untracked.txt"} {
		if !got[f] {
			t.Errorf("git repo missing %q in %v", f, files)
		}
	}
}
