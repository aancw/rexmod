package rex

import (
	"os"
	"testing"
)

func sample(t *testing.T, s string) any {
	t.Helper()
	v, err := decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestSocketPath(t *testing.T) {
	p, err := SocketPath("unix:///Users/a/Library/Application%20Support/rex/server.sock")
	if err != nil || p != "/Users/a/Library/Application Support/rex/server.sock" {
		t.Fatal(p, err)
	}
	if _, err := SocketPath("ws://localhost"); err != ErrNotUnixURL {
		t.Fatal(err)
	}
	long := "unix:///" + string(make([]byte, 200))
	if _, err := SocketPath(long); err != ErrNameTooLong {
		t.Fatal(err)
	}
}

func TestWelcome(t *testing.T) {
	w, err := ParseWelcome(sample(t, `{"type":"welcome","protocol":"rex.control.v1","limits":{"max_message_bytes":1048576},
	 "client":{"id":"client:x","reconnect_token":"t"},"api_revision":7,"new_field":[1]}`))
	if err != nil || w.MaxMessageBytes != 1048576 || w.APIRevision != 7 {
		t.Fatal(w, err)
	}
	if _, err := ParseWelcome(sample(t, `{"type":"welcome","protocol":"other","limits":{"max_message_bytes":1048576}}`)); err != ErrBadProtocol {
		t.Fatal(err)
	}
}

func TestResponses(t *testing.T) {
	m, _ := ParseMessage(sample(t, `{"type":"response","id":"3","seq":5,"payload":{"rex_version":"1.0"}}`))
	r := m.Response
	if v, _ := GetStr(r.Result.Ok, "rex_version"); r.ID != 3 || r.Seq != 5 || v != "1.0" {
		t.Fatal(r)
	}
	m, _ = ParseMessage(sample(t, `{"type":"response","id":"2","seq":0,"error":{"code":"not_found","message":"block not found","status":404}}`))
	e := m.Response.Result.Err
	if e.Code != "not_found" || e.Message != "block not found" || e.Status != 404 {
		t.Fatal(e)
	}
	if m, err := ParseMessage(sample(t, `{"type":"response","id":"abc","seq":0}`)); m != nil || err != nil {
		t.Fatal(m, err)
	}
}

func TestEvents(t *testing.T) {
	m, _ := ParseMessage(sample(t, `{"type":"event","seq":1,"payload":{"type":"session_created","payload":{"session":{"session_id":"session:s"}}}}`))
	if a := m.Event; a.Seq != 1 || a.Name != "session_created" || a.SessionID != nil {
		t.Fatal(a)
	}
	m, _ = ParseMessage(sample(t, `{"type":"event","seq":2,"payload":{"type":"session_event","payload":{"session_id":"session:s",
	 "event":{"type":"block_created","payload":{"block_id":"block:b"}}}}}`))
	b := m.Event
	if id, _ := GetStr(b.Payload, "block_id"); b.Name != "block_created" || *b.SessionID != "session:s" || id != "block:b" {
		t.Fatal(b)
	}
	m, _ = ParseMessage(sample(t, `{"type":"event","seq":3,"payload":{"type":"keymap_changed"}}`))
	if m.Event.Payload != nil {
		t.Fatal(m.Event)
	}
	if m, err := ParseMessage(sample(t, `{"type":"something_new","x":1}`)); m != nil || err != nil {
		t.Fatal(m, err)
	}
	if _, err := ParseMessage(sample(t, `{"seq":1}`)); err != ErrBadMessage {
		t.Fatal(err)
	}
}

func TestEncodeRequest(t *testing.T) {
	b, _ := EncodeRequest(4, "server.version", nil)
	if string(b) != `{"type":"request","id":"4","method":"server.version"}` {
		t.Fatal(string(b))
	}
	b, _ = EncodeRequest(5, "x", map[string]any{"a": "<&>"})
	if string(b) != `{"type":"request","id":"5","method":"x","payload":{"a":"<&>"}}` {
		t.Fatal(string(b))
	}
}

// waitFor reads until the response to id, skipping events.
func waitFor(t *testing.T, c *Client, id uint64) *Response {
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
			if m.Kind == KindResponse && m.Response.ID == id {
				return m.Response
			}
			if m.Kind == KindClosed {
				t.Fatal("closed")
			}
		}
		if ok, err := c.Read(); !ok {
			t.Fatal("hung up", err)
		}
	}
}

func TestLive(t *testing.T) {
	url := os.Getenv("REX_SERVER")
	if url == "" {
		t.Skip("REX_SERVER not set")
	}
	c, err := Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.MaxMessage < 65536 {
		t.Fatal(c.MaxMessage)
	}
	id, _ := c.Request("server.version", nil)
	if v, _ := GetStr(waitFor(t, c, id).Result.Ok, "rex_version"); v == "" {
		t.Fatal("no version")
	}
	bad, _ := c.Request("rexmod.no_such_method", map[string]any{"x": 1})
	e := waitFor(t, c, bad).Result.Err
	if e == nil || e.Code != "method_not_found" || e.Status != 404 {
		t.Fatal(e)
	}
}
