package nvim

import (
	"errors"
	"math"

	"github.com/alpcanaydin/rexmod/internal/editor/msgpack"
)

// ErrBadMessage is a message that isn't msgpack-RPC.
var ErrBadMessage = errors.New("nvim: bad rpc message")

// Message is a Request, Response or Notification.
type Message interface{ isMessage() }

// Request is [0, id, method, params].
type Request struct {
	ID     uint32
	Method string
	Params []any
}

// Response is [1, id, error, result].
type Response struct {
	ID uint32
	// Nil on success; from Neovim, [type, message].
	Err    any
	Result any
}

// Notification is [2, method, params].
type Notification struct {
	Method string
	Params []any
}

func (Request) isMessage()      {}
func (Response) isMessage()     {}
func (Notification) isMessage() {}

// ErrMessage returns Neovim's error message, or "" on success.
func (r Response) ErrMessage() string {
	switch e := r.Err.(type) {
	case nil:
		return ""
	case string:
		return e
	case []any:
		if len(e) >= 2 {
			if s, ok := e[1].(string); ok {
				return s
			}
		}
	}
	return "unknown error"
}

// Parse parses one whole message, as returned by msgpack.Framer.Next.
func Parse(b []byte) (Message, error) {
	v, n, err := msgpack.Decode(b)
	if err != nil {
		return nil, err
	}
	a, ok := v.([]any)
	if !ok || n != len(b) || len(a) == 0 {
		return nil, ErrBadMessage
	}
	typ, _ := a[0].(int64)
	switch {
	case typ == 0 && len(a) == 4:
		id, ok1 := id(a[1])
		method, ok2 := a[2].(string)
		params, ok3 := a[3].([]any)
		if ok1 && ok2 && ok3 {
			return Request{id, method, params}, nil
		}
	case typ == 1 && len(a) == 4:
		if id, ok := id(a[1]); ok {
			return Response{id, a[2], a[3]}, nil
		}
	case typ == 2 && len(a) == 3:
		method, ok1 := a[1].(string)
		params, ok2 := a[2].([]any)
		if ok1 && ok2 {
			return Notification{method, params}, nil
		}
	}
	return nil, ErrBadMessage
}

func id(v any) (uint32, bool) {
	n, ok := v.(int64)
	if !ok || n < 0 || n > math.MaxUint32 {
		return 0, false
	}
	return uint32(n), true
}

// appendRequest writes [0, id, method, args].
func appendRequest(b []byte, id uint32, method string, args []any) ([]byte, error) {
	b = msgpack.AppendArrayHeader(b, 4)
	b = msgpack.AppendInt(b, 0)
	b = msgpack.AppendInt(b, int64(id))
	b = msgpack.AppendStr(b, method)
	return msgpack.Append(b, args)
}
