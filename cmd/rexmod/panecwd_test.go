package main

import "testing"

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
