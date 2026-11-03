// Package msgpack encodes and decodes MessagePack.
//
// Decoded values are Go values: nil, bool, int64 (uint64 only above
// MaxInt64), float64, string, []byte, []any, Map and Ext.
package msgpack

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

var (
	// ErrShort means the bytes end mid-value: feed more.
	ErrShort = errors.New("msgpack: value cut short")
	// ErrInvalidByte is 0xc1, which is never used.
	ErrInvalidByte = errors.New("msgpack: invalid byte 0xc1")
	// ErrTooDeep is nesting past MaxDepth.
	ErrTooDeep = errors.New("msgpack: nested too deep")
)

// MaxDepth is how deeply arrays and maps may nest.
const MaxDepth = 32

// KV is one key-value pair of a Map.
type KV struct{ Key, Val any }

// Map is a decoded map, in wire order.
type Map []KV

// Get returns the value under string key k.
func (m Map) Get(k string) (any, bool) {
	for _, kv := range m {
		if s, ok := kv.Key.(string); ok && s == k {
			return kv.Val, true
		}
	}
	return nil, false
}

// Ext is an extension value. Neovim sends buffer (0), window (1) and
// tabpage (2) handles as ext wrapping an int.
type Ext struct {
	Type int8
	Data []byte
}

// Raw is already-encoded msgpack, written as is.
type Raw []byte

// Handle returns a buffer, window or tabpage handle: an ext wrapping an int,
// or a plain int.
func Handle(v any) (int64, bool) {
	switch v := v.(type) {
	case int64:
		return v, true
	case Ext:
		inner, n, err := Decode(v.Data)
		if err != nil || n != len(v.Data) {
			return 0, false
		}
		i, ok := inner.(int64)
		return i, ok
	}
	return 0, false
}

// Encode writes v to w.
func Encode(w io.Writer, v any) error {
	b, err := Append(nil, v)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// Append appends the encoding of v to b.
func Append(b []byte, v any) ([]byte, error) {
	switch v := v.(type) {
	case nil:
		return AppendNil(b), nil
	case bool:
		return AppendBool(b, v), nil
	case int:
		return AppendInt(b, int64(v)), nil
	case int8:
		return AppendInt(b, int64(v)), nil
	case int16:
		return AppendInt(b, int64(v)), nil
	case int32:
		return AppendInt(b, int64(v)), nil
	case int64:
		return AppendInt(b, v), nil
	case uint:
		return AppendUint(b, uint64(v)), nil
	case uint8:
		return AppendInt(b, int64(v)), nil
	case uint16:
		return AppendInt(b, int64(v)), nil
	case uint32:
		return AppendInt(b, int64(v)), nil
	case uint64:
		return AppendUint(b, v), nil
	case float32:
		return AppendFloat(b, float64(v)), nil
	case float64:
		return AppendFloat(b, v), nil
	case string:
		return AppendStr(b, v), nil
	case []byte:
		return AppendBin(b, v), nil
	case Raw:
		return append(b, v...), nil
	case Ext:
		return AppendExt(b, v.Type, v.Data), nil
	case []string:
		b = AppendArrayHeader(b, uint32(len(v)))
		for _, s := range v {
			b = AppendStr(b, s)
		}
		return b, nil
	case []any:
		b = AppendArrayHeader(b, uint32(len(v)))
		for _, e := range v {
			var err error
			if b, err = Append(b, e); err != nil {
				return b, err
			}
		}
		return b, nil
	case Map:
		b = AppendMapHeader(b, uint32(len(v)))
		for _, kv := range v {
			var err error
			if b, err = Append(b, kv.Key); err != nil {
				return b, err
			}
			if b, err = Append(b, kv.Val); err != nil {
				return b, err
			}
		}
		return b, nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b = AppendMapHeader(b, uint32(len(v)))
		for _, k := range keys {
			b = AppendStr(b, k)
			var err error
			if b, err = Append(b, v[k]); err != nil {
				return b, err
			}
		}
		return b, nil
	}
	return b, fmt.Errorf("msgpack: can't encode %T", v)
}

func AppendNil(b []byte) []byte { return append(b, 0xc0) }

func AppendBool(b []byte, v bool) []byte {
	if v {
		return append(b, 0xc3)
	}
	return append(b, 0xc2)
}

// AppendInt writes the smallest encoding that fits.
func AppendInt(b []byte, n int64) []byte {
	if n >= 0 {
		return AppendUint(b, uint64(n))
	}
	switch {
	case n >= -32:
		return append(b, byte(int8(n))) // negative fixint
	case n >= math.MinInt8:
		return append(b, 0xd0, byte(int8(n)))
	case n >= math.MinInt16:
		return binary.BigEndian.AppendUint16(append(b, 0xd1), uint16(n))
	case n >= math.MinInt32:
		return binary.BigEndian.AppendUint32(append(b, 0xd2), uint32(n))
	}
	return binary.BigEndian.AppendUint64(append(b, 0xd3), uint64(n))
}

func AppendUint(b []byte, n uint64) []byte {
	switch {
	case n <= 0x7f:
		return append(b, byte(n)) // positive fixint
	case n <= math.MaxUint8:
		return append(b, 0xcc, byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xcd), uint16(n))
	case n <= math.MaxUint32:
		return binary.BigEndian.AppendUint32(append(b, 0xce), uint32(n))
	}
	return binary.BigEndian.AppendUint64(append(b, 0xcf), n)
}

func AppendFloat(b []byte, f float64) []byte {
	return binary.BigEndian.AppendUint64(append(b, 0xcb), math.Float64bits(f))
}

func AppendStr(b []byte, s string) []byte {
	return append(AppendStrHeader(b, uint32(len(s))), s...)
}

// AppendStrHeader: follow with n bytes of string.
func AppendStrHeader(b []byte, n uint32) []byte {
	switch {
	case n <= 31:
		return append(b, 0xa0|byte(n)) // fixstr
	case n <= math.MaxUint8:
		return append(b, 0xd9, byte(n))
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xda), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, 0xdb), n)
}

func AppendBin(b []byte, v []byte) []byte {
	n := len(v)
	switch {
	case n <= math.MaxUint8:
		b = append(b, 0xc4, byte(n))
	case n <= math.MaxUint16:
		b = binary.BigEndian.AppendUint16(append(b, 0xc5), uint16(n))
	default:
		b = binary.BigEndian.AppendUint32(append(b, 0xc6), uint32(n))
	}
	return append(b, v...)
}

func AppendExt(b []byte, typ int8, data []byte) []byte {
	n := len(data)
	switch {
	case n == 1:
		b = append(b, 0xd4)
	case n == 2:
		b = append(b, 0xd5)
	case n == 4:
		b = append(b, 0xd6)
	case n == 8:
		b = append(b, 0xd7)
	case n == 16:
		b = append(b, 0xd8)
	case n <= math.MaxUint8:
		b = append(b, 0xc7, byte(n))
	case n <= math.MaxUint16:
		b = binary.BigEndian.AppendUint16(append(b, 0xc8), uint16(n))
	default:
		b = binary.BigEndian.AppendUint32(append(b, 0xc9), uint32(n))
	}
	return append(append(b, byte(typ)), data...)
}

// AppendArrayHeader: follow with n values.
func AppendArrayHeader(b []byte, n uint32) []byte {
	switch {
	case n <= 15:
		return append(b, 0x90|byte(n)) // fixarray
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xdc), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, 0xdd), n)
}

// AppendMapHeader: follow with n key-value pairs.
func AppendMapHeader(b []byte, n uint32) []byte {
	switch {
	case n <= 15:
		return append(b, 0x80|byte(n)) // fixmap
	case n <= math.MaxUint16:
		return binary.BigEndian.AppendUint16(append(b, 0xde), uint16(n))
	}
	return binary.BigEndian.AppendUint32(append(b, 0xdf), n)
}

// Len and At let other packages read a Map without importing this one.
func (m Map) Len() int                { return len(m) }
func (m Map) At(i int) (key, val any) { return m[i].Key, m[i].Val }

// HandleValue decodes a Neovim handle (Window, Buffer, Tabpage) from the ext payload.
func (e Ext) HandleValue() (int64, bool) { return Handle(e) }
