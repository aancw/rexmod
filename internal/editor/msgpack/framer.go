package msgpack

import "errors"

// Framer splits a byte stream back into whole msgpack messages.
type Framer struct {
	// bytes received: handed-out messages, then ones not yet handed out
	buf []byte
	// offset of the first byte not yet handed out
	start int
}

// Feed adds bytes read from the stream. Invalidates messages from Next.
func (f *Framer) Feed(b []byte) {
	// drop handed-out messages
	rest := copy(f.buf, f.buf[f.start:])
	f.buf = append(f.buf[:rest], b...)
	f.start = 0
}

// Next returns the next whole message, or nil until more is fed. Valid until
// Feed.
func (f *Framer) Next() ([]byte, error) {
	rest := f.buf[f.start:]
	if len(rest) == 0 {
		return nil, nil
	}
	n, err := Len(rest)
	if errors.Is(err, ErrShort) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	f.start += n
	return rest[:n:n], nil
}

// Buffered is the number of bytes held, handed out or not.
func (f *Framer) Buffered() int { return len(f.buf) }
