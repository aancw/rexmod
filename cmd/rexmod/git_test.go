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
