package msgpack

import (
	"encoding/binary"
	"math"
)

// Decode decodes the first value in b and returns it with its length.
// ErrShort means b ends mid-value. Strings, bins and exts are copied.
func Decode(b []byte) (v any, n int, err error) {
	d := decoder{b: b}
	v, err = d.value(0)
	return v, d.off, err
}

// Len returns the length of the first whole value in b without decoding it.
// ErrShort means it's cut off.
func Len(b []byte) (int, error) {
	d := decoder{b: b}
	err := d.skip(0)
	return d.off, err
}

type decoder struct {
	b   []byte
	off int
}

// header is one decoded header. For containers n is the item count; for
// str, bin and ext it's the byte count that follows.
type header struct {
	kind byte // one of the k* constants
	n    uint32
	v    any // scalar value
	ext  int8
}

const (
	kScalar = iota
	kArray
	kMap
	kStr
	kBin
	kExt
)

// headerLen is the header size in bytes, including the type byte. 0 for 0xc1.
func headerLen(c byte) int {
	switch {
	case c <= 0xbf, c == 0xc0, c == 0xc2, c == 0xc3, c >= 0xe0:
		return 1
	}
	switch c {
	case 0xc4, 0xcc, 0xd0, 0xd9:
		return 2 // 8-bit length or value
	case 0xc5, 0xcd, 0xd1, 0xda, 0xdc, 0xde:
		return 3 // 16-bit
	case 0xc6, 0xca, 0xce, 0xd2, 0xdb, 0xdd, 0xdf:
		return 5 // 32-bit
	case 0xcb, 0xcf, 0xd3:
		return 9 // 64-bit
	case 0xc7:
		return 3 // ext 8: length, then type
	case 0xc8:
		return 4 // ext 16
	case 0xc9:
		return 6 // ext 32
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8:
		return 2 // fixext: type only
	}
	return 0
}

func (d *decoder) header() (header, error) {
	if d.off >= len(d.b) {
		return header{}, ErrShort
	}
	c := d.b[d.off]
	need := headerLen(c)
	if need == 0 {
		return header{}, ErrInvalidByte
	}
	if len(d.b)-d.off < need {
		return header{}, ErrShort
	}
	h := d.b[d.off : d.off+need]
	d.off += need
	be := binary.BigEndian
	switch {
	case c <= 0x7f:
		return header{v: int64(c)}, nil
	case c <= 0x8f:
		return header{kind: kMap, n: uint32(c & 0x0f)}, nil
	case c <= 0x9f:
		return header{kind: kArray, n: uint32(c & 0x0f)}, nil
	case c <= 0xbf:
		return header{kind: kStr, n: uint32(c & 0x1f)}, nil
	case c >= 0xe0:
		return header{v: int64(int8(c))}, nil
	}
	switch c {
	case 0xc0:
		return header{v: nil}, nil
	case 0xc2:
		return header{v: false}, nil
	case 0xc3:
		return header{v: true}, nil
	case 0xc4:
		return header{kind: kBin, n: uint32(h[1])}, nil
	case 0xc5:
		return header{kind: kBin, n: uint32(be.Uint16(h[1:]))}, nil
	case 0xc6:
		return header{kind: kBin, n: be.Uint32(h[1:])}, nil
	case 0xc7:
		return header{kind: kExt, n: uint32(h[1]), ext: int8(h[2])}, nil
	case 0xc8:
		return header{kind: kExt, n: uint32(be.Uint16(h[1:])), ext: int8(h[3])}, nil
	case 0xc9:
		return header{kind: kExt, n: be.Uint32(h[1:]), ext: int8(h[5])}, nil
	case 0xca:
		return header{v: float64(math.Float32frombits(be.Uint32(h[1:])))}, nil
	case 0xcb:
		return header{v: math.Float64frombits(be.Uint64(h[1:]))}, nil
	case 0xcc:
		return header{v: int64(h[1])}, nil
	case 0xcd:
		return header{v: int64(be.Uint16(h[1:]))}, nil
	case 0xce:
		return header{v: int64(be.Uint32(h[1:]))}, nil
	case 0xcf:
		n := be.Uint64(h[1:])
		if n <= math.MaxInt64 {
			return header{v: int64(n)}, nil
		}
		return header{v: n}, nil
	case 0xd0:
		return header{v: int64(int8(h[1]))}, nil
	case 0xd1:
		return header{v: int64(int16(be.Uint16(h[1:])))}, nil
	case 0xd2:
		return header{v: int64(int32(be.Uint32(h[1:])))}, nil
	case 0xd3:
		return header{v: int64(be.Uint64(h[1:]))}, nil
	case 0xd4, 0xd5, 0xd6, 0xd7, 0xd8:
		return header{kind: kExt, n: 1 << (c - 0xd4), ext: int8(h[1])}, nil
	case 0xd9:
		return header{kind: kStr, n: uint32(h[1])}, nil
	case 0xda:
		return header{kind: kStr, n: uint32(be.Uint16(h[1:]))}, nil
	case 0xdb:
		return header{kind: kStr, n: be.Uint32(h[1:])}, nil
	case 0xdc:
		return header{kind: kArray, n: uint32(be.Uint16(h[1:]))}, nil
	case 0xdd:
		return header{kind: kArray, n: be.Uint32(h[1:])}, nil
	case 0xde:
		return header{kind: kMap, n: uint32(be.Uint16(h[1:]))}, nil
	}
	return header{kind: kMap, n: be.Uint32(h[1:])}, nil // 0xdf
}

// bytes takes the n bytes after a str, bin or ext header.
func (d *decoder) bytes(n uint32) ([]byte, error) {
	if uint64(len(d.b)-d.off) < uint64(n) {
		return nil, ErrShort
	}
	b := d.b[d.off : d.off+int(n)]
	d.off += int(n)
	return b, nil
}

func (d *decoder) value(depth int) (any, error) {
	h, err := d.header()
	if err != nil {
		return nil, err
	}
	switch h.kind {
	case kScalar:
		return h.v, nil
	case kStr, kBin, kExt:
		b, err := d.bytes(h.n)
		if err != nil {
			return nil, err
		}
		switch h.kind {
		case kStr:
			return string(b), nil
		case kBin:
			return append([]byte{}, b...), nil
		}
		return Ext{Type: h.ext, Data: append([]byte{}, b...)}, nil
	}
	if depth+1 >= MaxDepth {
		return nil, ErrTooDeep
	}
	// cap preallocation by what the bytes could hold
	c := min(int(h.n), len(d.b)-d.off)
	if h.kind == kArray {
		a := make([]any, 0, c)
		for range h.n {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			a = append(a, v)
		}
		return a, nil
	}
	m := make(Map, 0, c/2)
	for range h.n {
		k, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		v, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		m = append(m, KV{k, v})
	}
	return m, nil
}

// skip passes over one whole value.
func (d *decoder) skip(depth int) error {
	h, err := d.header()
	if err != nil {
		return err
	}
	switch h.kind {
	case kScalar:
		return nil
	case kStr, kBin, kExt:
		_, err := d.bytes(h.n)
		return err
	}
	if h.n == 0 {
		return nil
	}
	if depth+1 >= MaxDepth {
		return ErrTooDeep
	}
	n := uint64(h.n)
	if h.kind == kMap {
		n *= 2
	}
	for range n {
		if err := d.skip(depth + 1); err != nil {
			return err
		}
	}
	return nil
}
