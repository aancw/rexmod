// Package screen holds what Neovim draws for a ext_multigrid UI: its grids,
// highlights, windows and floats, built from "redraw" notifications, and
// renders them to a terminal as escape sequences. Ported from neorex's
// src/screen (MIT).
//
// # Decoded values
//
// The package does not decode msgpack itself. [Grids.ApplyRedraw] takes the
// "redraw" notification's params already decoded, as these Go values:
//
//   - nil, bool, string ([]byte is accepted where a string is expected)
//   - integers as int64 (uint64 and the other Go integer types are accepted)
//   - floats as float64 (float32 accepted)
//   - arrays as []any
//   - maps as any value satisfying [Map] (a map[string]any is accepted too)
//   - ext values that carry a Neovim handle (window, buffer, tabpage) as
//     any value satisfying [Handle]; a plain integer is accepted as a handle
//
// The integrator wires the msgpack package's map and ext types to [Map] and
// [Handle].
package screen

import (
	"errors"
	"math"
)

// Map is a decoded msgpack map, in wire order.
type Map interface {
	Len() int
	At(i int) (key, val any)
}

// Handle is a decoded msgpack ext value holding a Neovim handle. ok is
// false when the ext isn't a handle or its payload isn't an integer.
type Handle interface {
	HandleValue() (id int64, ok bool)
}

// ErrBadMessage reports a redraw event that doesn't have the shape Neovim
// documents.
var ErrBadMessage = errors.New("screen: bad redraw message")

func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case int:
		return int64(n), true
	case int32:
		return int64(n), true
	case int16:
		return int64(n), true
	case int8:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint8:
		return int64(n), true
	case uint:
		if uint64(n) > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

func expectInt(v any) (int64, error) {
	n, ok := toInt(v)
	if !ok {
		return 0, ErrBadMessage
	}
	return n, nil
}

func expectU32(v any) (uint32, error) {
	n, ok := toInt(v)
	if !ok || n < 0 || n > math.MaxUint32 {
		return 0, ErrBadMessage
	}
	return uint32(n), nil
}

func expectI32(v any) (int32, error) {
	n, ok := toInt(v)
	if !ok || n < math.MinInt32 || n > math.MaxInt32 {
		return 0, ErrBadMessage
	}
	return int32(n), nil
}

func expectStr(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	case []byte:
		return string(s), nil
	}
	return "", ErrBadMessage
}

func expectBool(v any) (bool, error) {
	b, ok := v.(bool)
	if !ok {
		return false, ErrBadMessage
	}
	return b, nil
}

func expectArray(v any) ([]any, error) {
	a, ok := v.([]any)
	if !ok {
		return nil, ErrBadMessage
	}
	return a, nil
}

// expectNumber: a float or an int, as Neovim sends positions.
func expectNumber(v any) (float64, error) {
	switch f := v.(type) {
	case float64:
		return f, nil
	case float32:
		return float64(f), nil
	}
	n, ok := toInt(v)
	if !ok {
		return 0, ErrBadMessage
	}
	return float64(n), nil
}

// expectHandle: an ext wrapping an int, or a plain int.
func expectHandle(v any) (int64, error) {
	if h, ok := v.(Handle); ok {
		id, ok := h.HandleValue()
		if !ok {
			return 0, ErrBadMessage
		}
		return id, nil
	}
	return expectInt(v)
}

type pair struct{ key, val any }

func expectMap(v any) ([]pair, error) {
	switch m := v.(type) {
	case Map:
		out := make([]pair, m.Len())
		for i := range out {
			out[i].key, out[i].val = m.At(i)
		}
		return out, nil
	case map[string]any:
		out := make([]pair, 0, len(m))
		for k, x := range m {
			out = append(out, pair{k, x})
		}
		return out, nil
	}
	return nil, ErrBadMessage
}

// ordered is an insertion-ordered map whose removal swaps the last entry
// into the gap, like Zig's AutoArrayHashMap.swapRemove: iteration order
// decides float stacking ties and group order.
type ordered[V any] struct {
	keys []uint32
	vals []V
	idx  map[uint32]int
}

func (m *ordered[V]) ptr(k uint32) *V {
	i, ok := m.idx[k]
	if !ok {
		return nil
	}
	return &m.vals[i]
}

func (m *ordered[V]) put(k uint32, v V) {
	if p := m.ptr(k); p != nil {
		*p = v
		return
	}
	if m.idx == nil {
		m.idx = map[uint32]int{}
	}
	m.idx[k] = len(m.keys)
	m.keys = append(m.keys, k)
	m.vals = append(m.vals, v)
}

func (m *ordered[V]) swapRemove(k uint32) bool {
	i, ok := m.idx[k]
	if !ok {
		return false
	}
	last := len(m.keys) - 1
	if i != last {
		m.keys[i], m.vals[i] = m.keys[last], m.vals[last]
		m.idx[m.keys[i]] = i
	}
	var zero V
	m.vals[last] = zero
	m.keys, m.vals = m.keys[:last], m.vals[:last]
	delete(m.idx, k)
	return true
}
