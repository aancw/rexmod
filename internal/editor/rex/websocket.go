package rex

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// A minimal RFC 6455 WebSocket client: the handshake and framing, no I/O.
// No extensions, so no compression.

var (
	ErrReservedBits           = errors.New("websocket: reserved bits set")
	ErrMaskedFrame            = errors.New("websocket: masked frame from server")
	ErrBadControlFrame        = errors.New("websocket: bad control frame")
	ErrMessageTooLarge        = errors.New("websocket: message too large")
	ErrUnfinishedMessage      = errors.New("websocket: unfinished message")
	ErrUnexpectedContinuation = errors.New("websocket: unexpected continuation")
	ErrBinaryMessage          = errors.New("websocket: binary message")
	ErrBadOpcode              = errors.New("websocket: bad opcode")
	ErrInvalidUTF8            = errors.New("websocket: invalid utf-8")
	ErrNotUpgraded            = errors.New("websocket: not upgraded")
	ErrBadAccept              = errors.New("websocket: bad accept")
	ErrBadProtocol            = errors.New("websocket: bad protocol")
	ErrUnexpectedExtension    = errors.New("websocket: unexpected extension")
)

type Opcode uint8

const (
	OpContinuation Opcode = 0
	OpText         Opcode = 1
	OpBinary       Opcode = 2
	OpClose        Opcode = 8
	OpPing         Opcode = 9
	OpPong         Opcode = 10
)

// Message is one whole message: Op is OpText, OpPing, OpPong or OpClose.
// Data is the text or ping/pong payload; Close is set for OpClose.
// Data is valid until the next Next or Feed.
type Message struct {
	Op    Opcode
	Data  []byte
	Close *Close
}

type Close struct {
	// HasCode is false when the frame had no body.
	HasCode bool
	Code    uint16
	Reason  string
}

type joined struct{ start, len int }

// WebSocket parses frames from the server.
type WebSocket struct {
	// Bytes received: handed-out frames, then ones not yet parsed.
	buf []byte
	// Offset of the first frame not yet parsed.
	pos int
	// A fragmented message being joined in place in buf.
	joined *joined
	// MaxMessage is the largest message accepted, in bytes; 0 means 1 MiB.
	MaxMessage int
}

func (ws *WebSocket) max() int {
	if ws.MaxMessage == 0 {
		return 1 << 20
	}
	return ws.MaxMessage
}

// Feed adds bytes read from the socket. Invalidates messages from Next.
func (ws *WebSocket) Feed(b []byte) {
	drop := ws.pos
	if ws.joined != nil {
		drop = ws.joined.start
	}
	n := copy(ws.buf, ws.buf[drop:])
	ws.buf = ws.buf[:n]
	ws.pos -= drop
	if ws.joined != nil {
		ws.joined.start = 0
	}
	ws.buf = append(ws.buf, b...)
}

// Next returns the next whole message, or nil until more is fed.
func (ws *WebSocket) Next() (*Message, error) {
	for {
		rest := ws.buf[ws.pos:]
		if len(rest) < 2 {
			return nil, nil
		}
		fin := rest[0]&0x80 != 0
		if rest[0]&0x70 != 0 {
			return nil, ErrReservedBits
		}
		op := Opcode(rest[0] & 0x0f)
		// servers never mask
		if rest[1]&0x80 != 0 {
			return nil, ErrMaskedFrame
		}
		len7 := rest[1] & 0x7f
		header := 2
		switch len7 {
		case 126:
			header = 4
		case 127:
			header = 10
		}
		if len(rest) < header {
			return nil, nil
		}
		var length uint64
		switch len7 {
		case 126:
			length = uint64(binary.BigEndian.Uint16(rest[2:4]))
		case 127:
			length = binary.BigEndian.Uint64(rest[2:10])
		default:
			length = uint64(len7)
		}
		if op >= 8 && (!fin || length > 125) {
			return nil, ErrBadControlFrame
		}
		soFar := 0
		if ws.joined != nil {
			soFar = ws.joined.len
		}
		if length > uint64(ws.max()-soFar) {
			return nil, ErrMessageTooLarge
		}
		n := int(length)
		if len(rest)-header < n {
			return nil, nil
		}
		start := ws.pos + header
		payload := ws.buf[start : start+n]
		ws.pos = start + n

		switch op {
		case OpText:
			if ws.joined != nil {
				return nil, ErrUnfinishedMessage
			}
			if fin {
				return textMessage(payload)
			}
			ws.joined = &joined{start: start, len: n}
		case OpContinuation:
			j := ws.joined
			if j == nil {
				return nil, ErrUnexpectedContinuation
			}
			// joined data always sits before this frame
			copy(ws.buf[j.start+j.len:], payload)
			j.len += n
			if fin {
				text := ws.buf[j.start : j.start+j.len]
				ws.joined = nil
				return textMessage(text)
			}
		case OpPing, OpPong:
			return &Message{Op: op, Data: payload}, nil
		case OpClose:
			if n == 1 {
				return nil, ErrBadControlFrame
			}
			if n == 0 {
				return &Message{Op: OpClose, Close: &Close{}}, nil
			}
			return &Message{Op: OpClose, Close: &Close{HasCode: true, Code: binary.BigEndian.Uint16(payload[:2]), Reason: string(payload[2:])}}, nil
		case OpBinary:
			return nil, ErrBinaryMessage
		default:
			return nil, ErrBadOpcode
		}
	}
}

func textMessage(b []byte) (*Message, error) {
	if !utf8.Valid(b) {
		return nil, ErrInvalidUTF8
	}
	return &Message{Op: OpText, Data: b}, nil
}

// NewKey returns a Sec-WebSocket-Key: 16 random bytes in base64.
func NewKey() string {
	var nonce [16]byte
	rand.Read(nonce[:])
	return base64.StdEncoding.EncodeToString(nonce[:])
}

// AcceptFor is the Sec-WebSocket-Accept a server must answer key with.
func AcceptFor(key string) string {
	h := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(h[:])
}

// WriteRequest writes the HTTP upgrade request.
func WriteRequest(w io.Writer, host, path, protocol, key string) error {
	_, err := fmt.Fprintf(w, "GET %s HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n"+
		"Sec-WebSocket-Protocol: %s\r\n"+
		"\r\n", path, host, key, protocol)
	return err
}

// HeadEnd is the length of the response head through its blank line, or -1 if it's cut off.
func HeadEnd(b []byte) int {
	i := bytes.Index(b, []byte("\r\n\r\n"))
	if i < 0 {
		return -1
	}
	return i + 4
}

// CheckResponse checks the server's response head: 101, the accept, and the subprotocol.
func CheckResponse(head, key, protocol string) error {
	lines := strings.Split(head, "\r\n")
	if !strings.HasPrefix(lines[0], "HTTP/1.1 101") {
		return ErrNotUpgraded
	}
	var upgrade, accepted, chosen bool
	for _, line := range lines[1:] {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.Trim(value, " \t")
		switch {
		case strings.EqualFold(name, "upgrade"):
			upgrade = strings.EqualFold(value, "websocket")
		case strings.EqualFold(name, "sec-websocket-accept"):
			accepted = value == AcceptFor(key)
		case strings.EqualFold(name, "sec-websocket-protocol"):
			chosen = value == protocol
		case strings.EqualFold(name, "sec-websocket-extensions"):
			// none were offered
			return ErrUnexpectedExtension
		}
	}
	if !upgrade {
		return ErrNotUpgraded
	}
	if !accepted {
		return ErrBadAccept
	}
	if !chosen {
		return ErrBadProtocol
	}
	return nil
}

// WriteFrame writes one final frame masked with a random mask.
func WriteFrame(w io.Writer, op Opcode, payload []byte) error {
	var mask [4]byte
	rand.Read(mask[:])
	return WriteFrameMasked(w, op, payload, mask)
}

// WriteFrameMasked writes one final frame masked with mask, in a single Write.
func WriteFrameMasked(w io.Writer, op Opcode, payload []byte, mask [4]byte) error {
	out := make([]byte, 0, 14+len(payload))
	out = append(out, 0x80|byte(op))
	switch {
	case len(payload) < 126:
		out = append(out, 0x80|byte(len(payload)))
	case len(payload) <= 0xffff:
		out = append(out, 0x80|126)
		out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	default:
		out = append(out, 0x80|127)
		out = binary.BigEndian.AppendUint64(out, uint64(len(payload)))
	}
	out = append(out, mask[:]...)
	for i, b := range payload {
		out = append(out, b^mask[i%4])
	}
	_, err := w.Write(out)
	return err
}

// WriteClose writes a close frame with code.
func WriteClose(w io.Writer, code uint16) error {
	var body [2]byte
	binary.BigEndian.PutUint16(body[:], code)
	return WriteFrame(w, OpClose, body[:])
}
