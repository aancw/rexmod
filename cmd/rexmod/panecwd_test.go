package main

import (
	"os"
	"testing"
)

func TestPaneCwdRecord(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	recordPaneCwd("block:abc", dir, []int{11, 22})
	if got := recordedPaneCwd("abc", 22); got != dir {
		t.Fatalf("same agent: got %q, want %q", got, dir)
	}
	if got := recordedPaneCwd("block:abc", 33); got != "" {
		t.Fatalf("another agent: got %q, want none", got)
	}
	recordPaneCwd("block:gone", dir+"/missing", []int{11})
	if got := recordedPaneCwd("block:gone", 11); got != "" {
		t.Fatalf("missing dir: got %q, want none", got)
	}
}

func TestWorktreeInUse(t *testing.T) {
	root := t.TempDir()
	for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "x"},
		{"worktree", "add", "-q", root + "/wt/a"}, {"worktree", "add", "-q", root + "/wt/ab"}} {
		if _, err := git(root, c...); err != nil {
			t.Fatal(err)
		}
	}
	root, _ = repoRoot(root)
	tr := t.TempDir() + "/t.jsonl"
	write := func(s string) {
		if err := os.WriteFile(tr, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"cmd":"cd ` + root + `/wt/ab && ls"} {"file_path":"` + root + `/wt/a/x.go"}`)
	if got := worktreeInUse(root, tr); got != root+"/wt/a" {
		t.Fatalf("got %q", got)
	}
	write(`{"file_path":"` + root + `/wt/ab/y.go"}`)
	if got := worktreeInUse(root, tr); got != root+"/wt/ab" {
		t.Fatalf("got %q", got)
	}
	write(`{"file_path":"` + root + `/wt/a/x.go"}` + "\n" +
		`{"type":"tool_result","content":"` + root + `/wt/ab  abc [b]"}`)
	if got := worktreeInUse(root, tr); got != root+"/wt/a" {
		t.Fatalf("tool output counted: got %q", got)
	}
	write("Worktree'yi `" + root + "/wt/a` konumuna taşıdım.")
	if got := worktreeInUse(root, tr); got != root+"/wt/a" {
		t.Fatalf("backtick: got %q", got)
	}
	write(`nothing`)
	if got := worktreeInUse(root, tr); got != root {
		t.Fatalf("got %q", got)
	}
}
