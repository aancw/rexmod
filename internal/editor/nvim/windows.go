package nvim

// Ported from neorex (MIT): src/nvim/windows.zig. windows.lua keeps every
// window except a hidden anchor external and reports the current window.
// These calls buffer requests; the caller flushes.

import (
	_ "embed"
	"errors"

	"github.com/alpcanaydin/rexmod/internal/editor/msgpack"
)

//go:embed windows.lua
var windowsLua string

func atLeast1(n uint32) int64 {
	if n < 1 {
		return 1
	}
	return int64(n)
}

// Setup installs the Lua on our channel chan and makes the current window
// external at cols x rows. Its response is [first window, anchor].
func (c *Client) Setup(chanID int64, cols, rows uint32) (uint32, error) {
	return c.ExecLua(windowsLua, chanID, int64(cols), int64(rows))
}

// Adopt makes ordinary window win external at cols x rows; anchors stay.
func (c *Client) Adopt(win int64, cols, rows uint32) error {
	_, err := c.ExecLua("neorex.adopt(...)", win, atLeast1(cols), atLeast1(rows))
	return err
}

// SizeWindow sizes external window win to fill cols x rows, statusline included.
func (c *Client) SizeWindow(win int64, cols, rows uint32) error {
	_, err := c.ExecLua("neorex.size(...)", win, atLeast1(cols), atLeast1(rows))
	return err
}

// PlaceGroup fits the floats hanging from editor floats tops to w x h at
// row, col; cur is where they are now: row, col, width, height.
func (c *Client) PlaceGroup(tops []int64, row, col int32, w, h uint32, cur [4]int64) (uint32, error) {
	ts := make([]any, len(tops))
	for i, t := range tops {
		ts[i] = t
	}
	return c.ExecLua("neorex.place_group(...)", ts, int64(row), int64(col), atLeast1(w), atLeast1(h),
		[]any{cur[0], cur[1], cur[2], cur[3]})
}

// CloseWindow closes win, keeping unsaved changes in its buffer.
func (c *Client) CloseWindow(win int64) error {
	_, err := c.ExecLua("neorex.close(...)", win)
	return err
}

func (c *Client) SetCurrentWin(win int64) (uint32, error) {
	return c.Request("nvim_set_current_win", win)
}

// AskChannel asks for our channel id: the response starts with it.
func (c *Client) AskChannel() (uint32, error) { return c.Request("nvim_get_api_info") }

// NoteKind is what the Lua tells the daemon.
type NoteKind int

const (
	NoteCurrent NoteKind = iota // the current window is now Win
	NoteReopen                  // Win is closing and reopening; keep its pane
	NoteSplit                   // a new window, and where Neovim put it
	NoteFloat                   // the current window is now float Win
	NoteEntered                 // float Win was just entered, before the redraw that shows it
)

type Note struct {
	Kind  NoteKind
	Win   int64
	Split Split
}

// Split is where a new window sits in Neovim's layout.
type Split struct {
	Win int64
	// Side by side (:vsplit), else one above the other.
	SideBySide bool
	// Left of or above the window beside it.
	Before bool
	// The window it was split from, or 0.
	From int64
}

var ErrBadNote = errors.New("nvim: bad note")

// ParseNote reads a notification from the Lua; ok is false for any other.
func ParseNote(method string, params []any) (Note, bool, error) {
	kinds := map[string]NoteKind{"neorex_win": NoteCurrent, "neorex_reopen": NoteReopen, "neorex_split": NoteSplit, "neorex_float": NoteFloat, "neorex_entered": NoteEntered}
	k, ok := kinds[method]
	if !ok {
		return Note{}, false, nil
	}
	need := 1
	if k == NoteSplit {
		need = 4
	}
	if len(params) < need {
		return Note{}, true, ErrBadNote
	}
	win, ok := msgpack.Handle(params[0])
	if !ok {
		return Note{}, true, ErrBadNote
	}
	n := Note{Kind: k, Win: win}
	if k == NoteSplit {
		sbs, ok1 := params[1].(bool)
		before, ok2 := params[2].(bool)
		from, ok3 := msgpack.Handle(params[3])
		if !ok1 || !ok2 || !ok3 {
			return Note{}, true, ErrBadNote
		}
		n.Split = Split{Win: win, SideBySide: sbs, Before: before, From: from}
	}
	return n, true, nil
}

// ParseSetup reads [first, anchor] from Setup's response.
func ParseSetup(result any) ([2]int64, error) {
	a, ok := result.([]any)
	if !ok || len(a) < 2 {
		return [2]int64{}, ErrBadNote
	}
	f, ok1 := msgpack.Handle(a[0])
	an, ok2 := msgpack.Handle(a[1])
	if !ok1 || !ok2 {
		return [2]int64{}, ErrBadNote
	}
	return [2]int64{f, an}, nil
}

// ParseChannel reads our channel id from AskChannel's response.
func ParseChannel(result any) (int64, error) {
	a, ok := result.([]any)
	if !ok || len(a) < 1 {
		return 0, ErrBadNote
	}
	n, ok := a[0].(int64)
	if !ok {
		return 0, ErrBadNote
	}
	return n, nil
}
