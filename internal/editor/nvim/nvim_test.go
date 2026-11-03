package nvim

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRequestEncoding(t *testing.T) {
	b, err := appendRequest(nil, 7, "nvim_input", []any{"<C-a>"})
	if err != nil || string(b) != "\x94\x00\x07\xaanvim_input\x91\xa5<C-a>" {
		t.Fatalf("% x %v", b, err)
	}
}

func parse(t *testing.T, s string) Message {
	t.Helper()
	m, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseResponse(t *testing.T) {
	r := parse(t, "\x94\x01\x01\xc0\x03").(Response)
	if r.ID != 1 || r.Err != nil || r.Result != int64(3) || r.ErrMessage() != "" {
		t.Fatalf("%#v", r)
	}
}

func TestParseErrorResponse(t *testing.T) {
	r := parse(t, "\x94\x01\x02\x92\x00\xa4boom\xc0").(Response)
	if r.ID != 2 || r.ErrMessage() != "boom" {
		t.Fatalf("%#v", r)
	}
}

func TestParseNotification(t *testing.T) {
	n := parse(t, "\x93\x02\xa6redraw\x91\x91\xa5flush").(Notification)
	if n.Method != "redraw" || !reflect.DeepEqual(n.Params, []any{[]any{"flush"}}) {
		t.Fatalf("%#v", n)
	}
}

func TestParseRequest(t *testing.T) {
	r := parse(t, "\x94\x00\x07\xa1x\x90").(Request)
	if r.ID != 7 || r.Method != "x" || len(r.Params) != 0 {
		t.Fatalf("%#v", r)
	}
}

func TestParseRejectsBadMessages(t *testing.T) {
	for _, s := range []string{
		"\x05",                 // not an array
		"\x93\x03\x01\x02",     // unknown type
		"\x93\x01\x01\xc0",     // response too short
		"\x94\x01\xff\xc0\xc0", // negative id
	} {
		if _, err := Parse([]byte(s)); err != ErrBadMessage {
			t.Errorf("% x: %v", s, err)
		}
	}
}

func spawnForTest(t *testing.T) *Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := Spawn("nvim", "--clean", "--embed")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// waitResponse reads until the response to id, skipping other messages.
func waitResponse(t *testing.T, c *Client, id uint32) Response {
	t.Helper()
	for {
		for {
			m, err := c.Next()
			if err != nil {
				t.Fatal(err)
			}
			if m == nil {
				break
			}
			if r, ok := m.(Response); ok && r.ID == id {
				return r
			}
		}
		if ok, err := c.Read(); !ok {
			t.Fatalf("nvim exited: %v", err)
		}
	}
}

func TestEval(t *testing.T) {
	c := spawnForTest(t)
	id, _ := c.Request("nvim_eval", "1+2")
	c.Flush()
	r := waitResponse(t, c, id)
	if r.Err != nil || r.Result != int64(3) {
		t.Fatalf("%#v", r)
	}
}

func TestErrorResponse(t *testing.T) {
	c := spawnForTest(t)
	id, _ := c.Request("nvim_eval", "no_such_variable")
	c.Flush()
	if r := waitResponse(t, c, id); !strings.Contains(r.ErrMessage(), "no_such_variable") {
		t.Fatalf("%#v", r)
	}
}

func TestResizeToSameSizeIsNotSent(t *testing.T) {
	c := spawnForTest(t)
	if err := c.UIAttach(40, 10, UIOptions{}); err != nil {
		t.Fatal(err)
	}
	before := c.nextID
	c.UITryResize(40, 10)
	if c.nextID != before {
		t.Fatal("sent")
	}
	c.UITryResize(30, 8)
	if c.nextID != before+1 {
		t.Fatal("not sent")
	}
}

func TestExecLua(t *testing.T) {
	c := spawnForTest(t)
	id, _ := c.ExecLua("return ... + 1", 41)
	c.Flush()
	if r := waitResponse(t, c, id); r.Result != int64(42) {
		t.Fatalf("%#v", r)
	}
}

func TestConnectToListeningNeovim(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	dir, err := os.MkdirTemp("", "nvt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	server := exec.Command("nvim", "--clean", "--headless", "--listen", path)
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { server.Process.Kill(); server.Wait() }()

	c, err := ConnectRetry(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id, _ := c.Request("nvim_eval", "1+2")
	c.Flush()
	if r := waitResponse(t, c, id); r.Result != int64(3) {
		t.Fatalf("%#v", r)
	}
}

func TestLargeReplyOverSeveralReads(t *testing.T) {
	c := spawnForTest(t)
	c.Request("nvim_get_api_info")
	id, _ := c.Request("nvim_get_api_info") // two in flight
	c.Flush()
	// [channel id, {version, functions, ...}]
	r := waitResponse(t, c, id)
	a, ok := r.Result.([]any)
	if !ok || len(a) != 2 || a[0].(int64) <= 0 {
		t.Fatalf("%#v", r.Result)
	}
}
