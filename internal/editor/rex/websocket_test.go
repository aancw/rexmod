package rex

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func serverFrame(fin bool, op Opcode, payload []byte) []byte {
	var out []byte
	b := byte(op)
	if fin {
		b |= 0x80
	}
	out = append(out, b)
	switch {
	case len(payload) < 126:
		out = append(out, byte(len(payload)))
	case len(payload) <= 0xffff:
		out = append(out, 126)
		out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	default:
		out = append(out, 127)
		out = binary.BigEndian.AppendUint64(out, uint64(len(payload)))
	}
	return append(out, payload...)
}

func expectText(t *testing.T, ws *WebSocket, want string) {
	t.Helper()
	m, err := ws.Next()
	if err != nil || m == nil || m.Op != OpText || string(m.Data) != want {
		t.Fatalf("want text %.20q, got %+v %v", want, m, err)
	}
}

func expectNone(t *testing.T, ws *WebSocket) {
	t.Helper()
	if m, err := ws.Next(); m != nil || err != nil {
		t.Fatalf("want nothing, got %+v %v", m, err)
	}
}

func TestAcceptExample(t *testing.T) {
	if got := AcceptFor("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatal(got)
	}
}

func TestRequestAndResponse(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	var b bytes.Buffer
	WriteRequest(&b, "localhost", "/api/control", "rex.control.v1", key)
	s := b.String()
	if !strings.HasPrefix(s, "GET /api/control HTTP/1.1\r\n") ||
		!strings.Contains(s, "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n") ||
		!strings.Contains(s, "Sec-WebSocket-Protocol: rex.control.v1\r\n") ||
		!strings.HasSuffix(s, "\r\n\r\n") {
		t.Fatal(s)
	}
	good := "HTTP/1.1 101 Switching Protocols\r\nupgrade: WebSocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=\r\nSec-WebSocket-Protocol: rex.control.v1\r\n\r\n"
	if HeadEnd([]byte(good+"\x81\x00")) != len(good) || HeadEnd([]byte(good[:len(good)-1])) != -1 {
		t.Fatal("headEnd")
	}
	if err := CheckResponse(good, key, "rex.control.v1"); err != nil {
		t.Fatal(err)
	}
	if err := CheckResponse(good, key, "other"); err != ErrBadProtocol {
		t.Fatal(err)
	}
	if err := CheckResponse(good, "AAAAAAAAAAAAAAAAAAAAAA==", "rex.control.v1"); err != ErrBadAccept {
		t.Fatal(err)
	}
	if err := CheckResponse("HTTP/1.1 400 Bad Request\r\n\r\n", key, "rex.control.v1"); err != ErrNotUpgraded {
		t.Fatal(err)
	}
}

func TestEachLengthForm(t *testing.T) {
	var ws WebSocket
	for _, n := range []int{0, 125, 126, 65535, 65536} {
		payload := bytes.Repeat([]byte("a"), n)
		frame := serverFrame(true, OpText, payload)
		want := 2
		if n >= 65536 {
			want = 10
		} else if n >= 126 {
			want = 4
		}
		if len(frame)-n != want {
			t.Fatal("header", n)
		}
		ws.Feed(frame)
		expectText(t, &ws, string(payload))
		expectNone(t, &ws)
	}
}

func TestSplitByteByByte(t *testing.T) {
	var ws WebSocket
	payload := strings.Repeat("x", 300)
	frame := serverFrame(true, OpText, []byte(payload))
	for _, b := range frame[:len(frame)-1] {
		ws.Feed([]byte{b})
		expectNone(t, &ws)
	}
	ws.Feed(frame[len(frame)-1:])
	expectText(t, &ws, payload)
}

func TestFragmentsWithPing(t *testing.T) {
	var ws WebSocket
	parts := [][]byte{
		serverFrame(false, OpText, []byte("hel")),
		serverFrame(false, OpContinuation, []byte("lo, ")),
		serverFrame(true, OpPing, []byte("p")),
		serverFrame(true, OpContinuation, []byte("world")),
		serverFrame(true, OpText, []byte("next")),
	}
	ws.Feed(parts[0])
	ws.Feed(parts[1])
	expectNone(t, &ws)
	// compaction keeps the joined part
	ws.Feed(parts[2])
	if m, _ := ws.Next(); m == nil || m.Op != OpPing || string(m.Data) != "p" {
		t.Fatal(m)
	}
	ws.Feed(append(append([]byte{}, parts[3]...), parts[4]...))
	expectText(t, &ws, "hello, world")
	expectText(t, &ws, "next")
}

func TestPingPongClose(t *testing.T) {
	var ws WebSocket
	ws.Feed([]byte("\x89\x02hi\x8a\x00\x88\x07\x03\xe9going\x88\x00"))
	if m, _ := ws.Next(); m.Op != OpPing || string(m.Data) != "hi" {
		t.Fatal(m)
	}
	if m, _ := ws.Next(); m.Op != OpPong || len(m.Data) != 0 {
		t.Fatal(m)
	}
	m, _ := ws.Next()
	if !m.Close.HasCode || m.Close.Code != 1001 || m.Close.Reason != "going" {
		t.Fatal(m.Close)
	}
	if m, _ := ws.Next(); m.Op != OpClose || m.Close.HasCode {
		t.Fatal(m)
	}
}

func TestOdditiesRejected(t *testing.T) {
	cases := []struct {
		in  string
		err error
	}{
		{"\x81\x81\x00\x00\x00\x00x", ErrMaskedFrame},
		{"\x82\x01x", ErrBinaryMessage},
		{"\xc1\x01x", ErrReservedBits},
		{"\x83\x00", ErrBadOpcode},
		{"\x09\x00", ErrBadControlFrame}, // fragmented ping
		{"\x88\x01x", ErrBadControlFrame},
		{"\x80\x01x", ErrUnexpectedContinuation},
		{"\x01\x01x\x81\x01y", ErrUnfinishedMessage},
		{"\x81\x02\xc3\x28", ErrInvalidUTF8},
		{"\x81\x7f\x00\x00\x00\x00\x00\x10\x00\x01", ErrMessageTooLarge},
	}
	for _, c := range cases {
		var ws WebSocket
		ws.Feed([]byte(c.in))
		if _, err := ws.Next(); !errors.Is(err, c.err) {
			t.Errorf("%q: got %v want %v", c.in, err, c.err)
		}
	}
	// fragments count toward the limit too
	ws := WebSocket{MaxMessage: 4}
	ws.Feed([]byte("\x01\x03abc\x80\x02de"))
	if _, err := ws.Next(); err != ErrMessageTooLarge {
		t.Fatal(err)
	}
}

func TestSentFramesMasked(t *testing.T) {
	mask := [4]byte{0x37, 0xfa, 0x21, 0x3d}
	var b bytes.Buffer
	// RFC 6455 section 5.7: masked "Hello"
	WriteFrameMasked(&b, OpText, []byte("Hello"), mask)
	if !bytes.Equal(b.Bytes(), []byte("\x81\x85\x37\xfa\x21\x3d\x7f\x9f\x4d\x51\x58")) {
		t.Fatalf("%x", b.Bytes())
	}
	for _, n := range []int{126, 300, 65536} {
		b.Reset()
		WriteFrameMasked(&b, OpPong, bytes.Repeat([]byte("z"), n), mask)
		out := b.Bytes()
		header := 8
		if n >= 65536 {
			header = 14
		}
		if len(out) != header+n || out[0] != 0x8a || !bytes.Equal(out[header-4:header], mask[:]) {
			t.Fatal("frame", n)
		}
		for i, c := range out[header:] {
			if c^mask[i%4] != 'z' {
				t.Fatal("mask", i)
			}
		}
	}
	b.Reset()
	WriteClose(&b, 1000)
	out := b.Bytes()
	if out[0] != 0x88 || out[1] != 0x82 || out[6]^out[2] != 0x03 || out[7]^out[3] != 0xe8 {
		t.Fatalf("%x", out)
	}
}
