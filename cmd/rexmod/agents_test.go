package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentOf(t *testing.T) {
	cases := []struct {
		name string
		p    *procInfo
		want string
	}{
		{"claude by name", &procInfo{Name: "claude", Path: "/opt/homebrew/Caskroom/claude-code/2.1/claude"}, "Claude"},
		{"claude by path", &procInfo{Name: "node", Path: "/usr/lib/node_modules/@anthropic-ai/claude-code/cli.js"}, "Claude"},
		{"codex", &procInfo{Name: "codex", Invoke: "/opt/homebrew/bin/codex"}, "Codex"},
		{"opencode", &procInfo{Name: "opencode", Argv0: "opencode"}, "OpenCode"},
		{"pi on node", &procInfo{Name: "node", Argv0: "pi", Path: "/usr/local/bin/node"}, "Pi"},
		{"agy", &procInfo{Name: "agy", Invoke: "/opt/homebrew/bin/agy"}, "Antigravity"},
		{"login shell", &procInfo{Name: "fish", Argv0: "-fish"}, ""},
		{"nvim", &procInfo{Name: "nvim"}, ""},
		{"nil", nil, ""},
	}
	for _, c := range cases {
		if got := agentOf(c.p); got != c.want {
			t.Errorf("%s: agentOf = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCommentFor(t *testing.T) {
	pane := Comment{TargetBlock: "block:aaa"}
	loose := Comment{}
	if !pane.For("block:aaa") || pane.For("block:bbb") {
		t.Error("a pane note goes to its own pane only")
	}
	if !pane.For("") {
		t.Error("an empty block means any agent")
	}
	if pane.For(outsideRex) {
		t.Error("an agent outside Rex must not get a pane's note")
	}
	if !loose.For(outsideRex) || !loose.For("block:bbb") {
		t.Error("a note made outside Rex goes to anyone")
	}
}

func writeTurn(t *testing.T, dir, file string, turn Turn) {
	t.Helper()
	if err := writeJSONAtomic(filepath.Join(dir, file), turn); err != nil {
		t.Fatal(err)
	}
}

func TestLatestTurnPerAgent(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "init", "-q"); err != nil {
		t.Skip("git not available:", err)
	}
	d, err := turnsDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	at := func(min int) string { return time.Date(2026, 1, 1, 10, min, 0, 0, time.UTC).Format(time.RFC3339Nano) }
	writeTurn(t, d, "a1.json", Turn{SessionID: "a1", Block: "block:aaa", Start: "t1", StartedAt: at(1)})
	writeTurn(t, d, "a2.json", Turn{SessionID: "a2", Block: "block:aaa", Start: "t2", StartedAt: at(3)})
	writeTurn(t, d, "b1.json", Turn{SessionID: "b1", Block: "block:bbb", Start: "t3", StartedAt: at(5)})

	if got := latestTurn(repo, "block:aaa"); got == nil || got.SessionID != "a2" {
		t.Errorf("agent aaa: got %+v, want its newest turn a2", got)
	}
	if got := latestTurn(repo, "aaa"); got == nil || got.SessionID != "a2" {
		t.Errorf("a block ID suffix should match too, got %+v", got)
	}
	if got := latestTurn(repo, "block:ccc"); got != nil {
		t.Errorf("another agent's turn must not stand in, got %+v", got)
	}
	if got := latestTurn(repo, ""); got == nil || got.SessionID != "b1" {
		t.Errorf("any agent: got %+v, want the newest turn b1", got)
	}
	writeTurn(t, d, "x.json", Turn{SessionID: "x", Start: "t4", StartedAt: at(7)})
	if got := latestTurn(repo, "block:ccc"); got == nil || got.SessionID != "x" {
		t.Errorf("a turn recorded outside Rex stands in, got %+v", got)
	}
}
