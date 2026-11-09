package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAgyInput(t *testing.T) {
	var h agyHook
	payload := `{"conversationId":"c1","workspacePaths":["/w/a","/w/b"],"transcriptPath":"/t.jsonl","invocationNum":2,"lastUserInput":"hi"}`
	if err := json.Unmarshal([]byte(payload), &h); err != nil {
		t.Fatal(err)
	}
	in := h.input()
	if in.SessionID != "c1" || in.Cwd != "/w/a" || in.TranscriptPath != "/t.jsonl" || h.InvocationNum != 2 {
		t.Fatalf("got %+v from %+v", in, h)
	}
	if got := (agyHook{}).input().Cwd; got != "" {
		t.Fatalf("no workspace: got cwd %q", got)
	}
}

func TestAgyMarksTurn(t *testing.T) {
	running := &Turn{Start: "t0", Prompt: promptKey("fix it")}
	ended := &Turn{Start: "t0", End: "t1", Prompt: promptKey("fix it")}
	cases := []struct {
		name  string
		event string
		h     agyHook
		cur   *Turn
		want  bool
	}{
		{"first model call", "prompt", agyHook{LastUserInput: "fix it"}, nil, true},
		{"later model call", "prompt", agyHook{LastUserInput: "fix it", InvocationNum: 1}, running, false},
		{"model call after a Stop hook said continue", "prompt", agyHook{LastUserInput: "fix it", InvocationNum: 2}, ended, false},
		{"subagent", "prompt", agyHook{}, nil, false},
		{"run resumed after a subagent", "prompt", agyHook{LastUserInput: "fix it"}, running, false},
		{"new message while one runs", "prompt", agyHook{LastUserInput: "and the tests"}, running, true},
		{"same message after the turn ended", "prompt", agyHook{LastUserInput: "fix it"}, ended, true},
		{"stop, all done", "stop", agyHook{FullyIdle: true}, running, true},
		{"stop while a subagent runs", "stop", agyHook{}, running, false},
	}
	for _, c := range cases {
		if got := c.h.marksTurn(c.event, c.cur); got != c.want {
			t.Errorf("%s: marksTurn = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAgyReply(t *testing.T) {
	b, _ := json.Marshal(agyReply(""))
	if string(b) != `{}` {
		t.Errorf("nothing to say: got %s, want {}", b)
	}
	b, _ = json.Marshal(agyReply("note"))
	if want := `{"injectSteps":[{"systemMessage":{"systemMessage":"note"}}]}`; string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

// TestAgyHookTurn replays the hook runs agy 1.5.3 makes for one user
// message whose agent hands work to a subagent and waits for it.
func TestAgyHookTurn(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("REX_BLOCK", "block:agy")
	repo := t.TempDir()
	if _, err := git(repo, "init", "-q"); err != nil {
		t.Skip("git not available:", err)
	}
	repo, _ = repoRoot(repo)
	write := func(name string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	note := func(text string) {
		write("a.txt")
		if _, err := addComment(repo, "a.txt", 1, 1, text, "", "block:agy", false); err != nil {
			t.Fatal(err)
		}
	}
	run := func(event, payload string) string {
		t.Helper()
		var m map[string]any
		if err := json.Unmarshal([]byte(payload), &m); err != nil {
			t.Fatal(err)
		}
		m["workspacePaths"] = []string{repo}
		b, _ := json.Marshal(m)
		return hookTurn(event, "agy", b)
	}
	const main, sub = `"conversationId":"main"`, `"conversationId":"sub"`
	const ask = `"lastUserInput":"delegate it"`

	note("first note")
	if got := run("prompt", `{`+main+`,`+ask+`,"invocationNum":0}`); !strings.Contains(got, "first note") {
		t.Fatalf("first model call: got %q, want the pending note", got)
	}
	note("second note")
	if got := run("prompt", `{`+main+`,`+ask+`,"invocationNum":1}`); got != "" {
		t.Fatalf("later model call: got %q", got)
	}
	if got := run("prompt", `{`+sub+`,"lastUserInput":"","invocationNum":0}`); got != "" {
		t.Fatalf("subagent: got %q, the note is not for it", got)
	}
	write("sub.txt")
	run("stop", `{`+main+`,"fullyIdle":false}`)
	run("stop", `{`+sub+`,"fullyIdle":true}`)
	if got := run("prompt", `{`+main+`,`+ask+`,"invocationNum":0}`); got != "" {
		t.Fatalf("resumed run: got %q, want no new turn", got)
	}
	write("main.txt")
	run("stop", `{`+main+`,"fullyIdle":true}`)

	d, _ := turnsDir(repo)
	if _, err := os.Stat(turnFile(d, "sub")); err == nil {
		t.Error("the subagent got a turn of its own")
	}
	c, err := computeChanges(repo, "last-turn", "", "block:agy")
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, f := range c.Files {
		files = append(files, f.Path)
	}
	if c.Running || !slices.Contains(files, "sub.txt") || !slices.Contains(files, "main.txt") {
		t.Fatalf("last turn: running=%v files=%v, want both agents' files, ended", c.Running, files)
	}

	// The same words again, after the turn ended, are a new turn.
	if got := run("prompt", `{`+main+`,`+ask+`,"invocationNum":0}`); !strings.Contains(got, "second note") {
		t.Fatalf("next message: got %q, want the note left meanwhile", got)
	}
}

func TestWorktreeInUseAgy(t *testing.T) {
	root := t.TempDir()
	for _, c := range [][]string{{"init", "-q", "-b", "main"}, {"commit", "-q", "--allow-empty", "-m", "x"},
		{"worktree", "add", "-q", root + "/wt/a"}, {"worktree", "add", "-q", root + "/wt/b"}} {
		if _, err := git(root, c...); err != nil {
			t.Fatal(err)
		}
	}
	root, _ = repoRoot(root)
	tr := t.TempDir() + "/transcript_full.jsonl"
	lines := `{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","tool_calls":[{"name":"write_file","args":{"TargetFile":"` + root + `/wt/a/x.go"}}]}` + "\n" +
		`{"step_index":4,"source":"MODEL","type":"GENERIC","content":"Created At: now\n` + root + `/wt/b  abc [b]"}` + "\n"
	if err := os.WriteFile(tr, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := worktreeInUse(root, tr); got != root+"/wt/a" {
		t.Fatalf("got %q, want the worktree the agent wrote to, not one a tool listed", got)
	}
}
