package main

import "testing"

func TestParseClaudeUsage(t *testing.T) {
	u := parseClaudeUsage([]byte(`{"other":1,"cachedUsageUtilization":{"fetchedAtMs":1793634961647,"utilization":{
		"five_hour":{"utilization":12,"resets_at":"2026-11-02T20:30:00+00:00"},
		"seven_day":{"utilization":8.5,"resets_at":"2026-11-09T11:00:00+00:00"},
		"seven_day_opus":null,"extra_usage":{"utilization":"n/a","daily":[1]}}}}`))
	if u == nil || u.Source != "Claude" || len(u.Windows) != 2 {
		t.Fatalf("got %+v", u)
	}
	if w := u.Windows[0]; w.Label != "5h" || w.Used != 12 || w.ResetsAt != "2026-11-02T20:30:00+00:00" {
		t.Errorf("5h: %+v", w)
	}
	if w := u.Windows[1]; w.Label != "7d" || w.Used != 8.5 {
		t.Errorf("7d: %+v", w)
	}
	if u.FetchedAt != "2026-11-02T15:56:01Z" {
		t.Errorf("fetched at %q", u.FetchedAt)
	}
	for _, in := range []string{`{}`, `{"cachedUsageUtilization":{"utilization":{}}}`, `not json`} {
		if u := parseClaudeUsage([]byte(in)); u != nil {
			t.Errorf("%s: want nothing, got %+v", in, u)
		}
	}
}

func TestEtimeSeconds(t *testing.T) {
	for in, want := range map[string]int{"00:05": 5, "12:34": 754, "01:02:03": 3723, "2-01:00:00": 176400} {
		if got := etimeSeconds(in); got != want {
			t.Errorf("%s: %d, want %d", in, got, want)
		}
	}
}

func TestJobsAndDescendants(t *testing.T) {
	rows := parsePS(`  100     1   10:00 /usr/bin/login
  101   100   09:59 -zsh
  200   101   01:00 /Users/x/.local/bin/claude
  201   101   00:30 /usr/bin/python3
  300   200   00:20 node
  999     1   00:01 other`)
	j := jobs(rows, 100)
	if len(j) != 2 || j[0].Name != "claude" || j[0].Seconds != 60 || j[1].Name != "python3" {
		t.Fatalf("jobs %+v", j)
	}
	d := descendants(rows, 100)
	if len(d) != 5 {
		t.Fatalf("descendants %v", d)
	}
	for _, pid := range d {
		if pid == 999 {
			t.Errorf("an unrelated process is a descendant")
		}
	}
}

func TestParseLsof(t *testing.T) {
	p := parseLsof("p201\ncPython\nn*:8765\nn[::1]:8765\np300\ncnode\nn127.0.0.1:3000\n")
	if len(p) != 2 || p[0].Port != 3000 || p[0].Name != "node" || p[1].Port != 8765 || p[1].Pid != 201 {
		t.Fatalf("got %+v", p)
	}
}
