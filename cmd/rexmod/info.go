package main

import (
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Info is what the panel's Info tab shows beside the working directory: the
// followed agent's state, session and plan usage, what runs in its pane, and
// the ports the window's panes listen on. Everything is read locally; nothing
// here asks a server.
type Info struct {
	Agent     *AgentInfo `json:"agent,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
	Processes []Proc     `json:"processes"`
	Ports     []Port     `json:"ports"`
}

type AgentInfo struct {
	Name      string `json:"name"`
	BlockID   string `json:"block_id"`
	Status    string `json:"status,omitempty"` // working, blocked or idle, as the agent reports it to Rex
	Message   string `json:"message,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Resume    string `json:"resume,omitempty"` // command that continues the session elsewhere
}

// Usage is the plan's rate-limit windows, as the agent last fetched them.
type Usage struct {
	Source    string        `json:"source"`
	FetchedAt string        `json:"fetched_at,omitempty"`
	Windows   []UsageWindow `json:"windows"`
}

type UsageWindow struct {
	Label    string  `json:"label"` // 5h, 7d
	Used     float64 `json:"used"`  // percent
	ResetsAt string  `json:"resets_at,omitempty"`
}

type Proc struct {
	Pid     int    `json:"pid"`
	Name    string `json:"name"`
	Seconds int    `json:"seconds"` // running for
}

type Port struct {
	Port int    `json:"port"`
	Pid  int    `json:"pid"`
	Name string `json:"name"`
	Pane string `json:"pane,omitempty"` // block id
}

func cmdInfo(args []string) error {
	fs := flag.NewFlagSet("info", flag.ExitOnError)
	s, w := targetFlags(fs)
	parse(fs, args)
	info := Info{Processes: []Proc{}, Ports: []Port{}}
	t, err := resolveTarget(*s, *w)
	if err != nil {
		return printJSON(info)
	}
	table := processTable()
	if t.AgentBlockID != "" {
		a := &AgentInfo{Name: t.Agent, BlockID: t.AgentBlockID}
		if st, msg := programStatus(t.SessionID, t.AgentBlockID); st != "" {
			a.Status, a.Message = st, msg
		}
		if bp, err := blockProc(t.SessionID, t.AgentBlockID); err == nil {
			if bp.Child != nil {
				info.Processes = jobs(table, bp.Child.Pid)
			}
			if t.Agent == "Claude" && bp.Foreground != nil {
				if id := claudeSession(bp.Foreground.Pid); id != "" {
					a.SessionID = id
					a.Resume = "claude --resume " + id
				}
			}
		}
		info.Agent = a
		if t.Agent == "Claude" {
			info.Usage = claudeUsage()
		}
	}
	if v, err := sessionView(t.SessionID); err == nil {
		if win := v.window(t.WindowID); win != nil {
			owners := map[int]string{}
			for _, b := range win.blocks() {
				if !strings.HasSuffix(b.Flavor, "terminal.shell") {
					continue
				}
				if bp, err := blockProc(t.SessionID, b.BlockID); err == nil && bp.Child != nil {
					for _, pid := range descendants(table, bp.Child.Pid) {
						owners[pid] = b.BlockID
					}
				}
			}
			info.Ports = listeningPorts(owners)
		}
	}
	return printJSON(info)
}

// programStatus is the latest status a program in the pane reported to Rex
// (OSC 7501: working, blocked…), with its message.
func programStatus(session, block string) (string, string) {
	var r struct {
		Records []struct {
			State     string `json:"state"`
			Msg       string `json:"msg"`
			UpdatedAt string `json:"updated_at"`
		} `json:"records"`
	}
	if err := rexJSON(&r, "block", "call", "-s", session, "-b", block, "program_status"); err != nil || len(r.Records) == 0 {
		return "", ""
	}
	sort.Slice(r.Records, func(i, j int) bool { return r.Records[i].UpdatedAt > r.Records[j].UpdatedAt })
	return r.Records[0].State, r.Records[0].Msg
}

func claudeDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// claudeSession reads the session id Claude Code records for its process.
func claudeSession(pid int) string {
	data, err := os.ReadFile(filepath.Join(claudeDir(), "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return ""
	}
	var s struct {
		SessionID string `json:"sessionId"`
	}
	if json.Unmarshal(data, &s) != nil {
		return ""
	}
	return s.SessionID
}

// claudeUsage reads the plan usage Claude Code caches in .claude.json each
// time it fetches it (it does so on its own while it runs).
func claudeUsage() *Usage {
	path := filepath.Join(filepath.Dir(claudeDir()), ".claude.json")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		path = filepath.Join(d, ".claude.json")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return parseClaudeUsage(data)
}

func parseClaudeUsage(data []byte) *Usage {
	// Only the two windows are read: the other entries change shape from one
	// Claude Code version to the next, and one odd field mustn't lose them.
	var c struct {
		Cached *struct {
			FetchedAtMs int64                      `json:"fetchedAtMs"`
			Utilization map[string]json.RawMessage `json:"utilization"`
		} `json:"cachedUsageUtilization"`
	}
	if json.Unmarshal(data, &c) != nil || c.Cached == nil {
		return nil
	}
	u := &Usage{Source: "Claude", Windows: []UsageWindow{}}
	if c.Cached.FetchedAtMs > 0 {
		u.FetchedAt = time.UnixMilli(c.Cached.FetchedAtMs).UTC().Format(time.RFC3339)
	}
	for _, w := range []struct{ key, label string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		var v struct {
			Utilization *float64 `json:"utilization"`
			ResetsAt    string   `json:"resets_at"`
		}
		if json.Unmarshal(c.Cached.Utilization[w.key], &v) == nil && v.Utilization != nil {
			u.Windows = append(u.Windows, UsageWindow{Label: w.label, Used: *v.Utilization, ResetsAt: v.ResetsAt})
		}
	}
	if len(u.Windows) == 0 {
		return nil
	}
	return u
}

type psRow struct {
	pid, ppid, seconds int
	name               string
}

// processTable is every process: pid, parent, how long it has run, name.
func processTable() []psRow {
	out, err := exec.Command("/bin/ps", "-A", "-o", "pid=,ppid=,etime=,comm=").Output()
	if err != nil {
		return nil
	}
	return parsePS(string(out))
}

func parsePS(out string) []psRow {
	var rows []psRow
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		if e1 != nil || e2 != nil {
			continue
		}
		name := filepath.Base(strings.Join(f[3:], " "))
		rows = append(rows, psRow{pid: pid, ppid: ppid, seconds: etimeSeconds(f[2]), name: strings.TrimPrefix(name, "-")})
	}
	return rows
}

// etimeSeconds parses ps's [[dd-]hh:]mm:ss.
func etimeSeconds(s string) int {
	days := 0
	if i := strings.Index(s, "-"); i >= 0 {
		days, _ = strconv.Atoi(s[:i])
		s = s[i+1:]
	}
	total := 0
	for _, p := range strings.Split(s, ":") {
		n, _ := strconv.Atoi(p)
		total = total*60 + n
	}
	return days*86400 + total
}

// jobs is what runs in a pane: the children of its shell, the shell being
// the child of the pane's login process.
func jobs(rows []psRow, login int) []Proc {
	shell := 0
	for _, r := range rows {
		if r.ppid == login {
			shell = r.pid
			break
		}
	}
	out := []Proc{}
	if shell == 0 {
		return out
	}
	for _, r := range rows {
		if r.ppid == shell {
			out = append(out, Proc{Pid: r.pid, Name: r.name, Seconds: r.seconds})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Pid < out[j].Pid })
	return out
}

// descendants is root and every process below it.
func descendants(rows []psRow, root int) []int {
	kids := map[int][]int{}
	for _, r := range rows {
		kids[r.ppid] = append(kids[r.ppid], r.pid)
	}
	out := []int{root}
	for i := 0; i < len(out); i++ {
		out = append(out, kids[out[i]]...)
	}
	return out
}

// listeningPorts is the TCP ports those processes listen on, one entry per
// port, with the pane each belongs to.
func listeningPorts(owners map[int]string) []Port {
	ports := []Port{}
	if len(owners) == 0 {
		return ports
	}
	out, err := exec.Command("/usr/sbin/lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn").Output()
	if err != nil && len(out) == 0 {
		return ports
	}
	for _, p := range parseLsof(string(out)) {
		if pane, ok := owners[p.Pid]; ok {
			p.Pane = pane
			ports = append(ports, p)
		}
	}
	return ports
}

// parseLsof reads lsof -F pcn output: p<pid>, c<command>, n<addr:port>.
func parseLsof(out string) []Port {
	var ports []Port
	seen := map[int]bool{}
	pid, name := 0, ""
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		switch l[0] {
		case 'p':
			pid, _ = strconv.Atoi(l[1:])
		case 'c':
			name = l[1:]
		case 'n':
			i := strings.LastIndex(l, ":")
			if i < 0 {
				continue
			}
			port, err := strconv.Atoi(l[i+1:])
			if err != nil || seen[port] {
				continue
			}
			seen[port] = true
			ports = append(ports, Port{Port: port, Pid: pid, Name: name})
		}
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i].Port < ports[j].Port })
	return ports
}
