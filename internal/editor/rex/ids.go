package rex

import "errors"

// Rex ids: a "block:" or "layer:" prefix and a UUID, 42 bytes in all.
// Windows ids ("window:" and a UUID) are 43 bytes.

var (
	ErrBadRexBlock = errors.New("rex: bad block id")
	ErrBadRexLayer = errors.New("rex: bad layer id")
)

// IDLen is the length of a block or layer id.
const IDLen = 42

// WindowIDLen is the length of a window id.
const WindowIDLen = 43

// Block checks that id has the length of a block id.
func Block(id string) (string, error) {
	if len(id) != IDLen {
		return "", ErrBadRexBlock
	}
	return id, nil
}

// Layer checks that id has the length of a layer id.
func Layer(id string) (string, error) {
	if len(id) != IDLen {
		return "", ErrBadRexLayer
	}
	return id, nil
}

// SameBlock reports whether a and b name the same block.
func SameBlock(a, b string) bool { return a == b }
