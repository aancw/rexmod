package screen

import (
	"bytes"
	"testing"
)

func TestScreenFloatsMoveIntoPane(t *testing.T) {
	v := NewPaneView(20, 10)
	rects := []Rect{
		// over the pane at 0, 30 of a wider screen
		{Grid: 3, Row: 2, Col: 35, Width: 4, Height: 2, OnScreen: true},
		// past its right edge: moved back in
		{Grid: 4, Row: 0, Col: 70, Width: 8, Height: 1, OnScreen: true},
		// over a window: left alone
		{Grid: 5, Row: 1, Col: 1, Width: 2, Height: 1},
		// on the messages: kept to the bottom
		{Grid: 6, Row: 25, Col: 2, Width: 5, Height: 3, OnScreen: true, OnMsg: true},
	}
	v.fitScreen(rects, [2]int32{0, 30}, 30)
	eq(t, rects[0].Row, 2)
	eq(t, rects[0].Col, 5)
	eq(t, rects[1].Col, 12)
	eq(t, rects[2].Col, 1)
	eq(t, rects[3].Row, 5)
	eq(t, rects[3].Col, 2)
}

func TestCommandLineAndMenuMoveTogether(t *testing.T) {
	// a pane 40 wide at column 40 of an 80 wide screen
	v := NewPaneView(40, 20)
	rects := []Rect{
		// noice's box, wider than the pane, and its text inside the border
		{Grid: 3, Row: 2, Col: 8, Width: 64, Height: 3, OnScreen: true, Top: 3},
		{Grid: 4, Row: 3, Col: 10, Width: 60, Height: 1, OnScreen: true, Top: 3},
		// blink's menu over the box's bottom border, and its scrollbar
		{Grid: 5, Row: 4, Col: 8, Width: 39, Height: 10, OnScreen: true, Top: 5},
		{Grid: 6, Row: 4, Col: 47, Width: 1, Height: 10, OnScreen: true, Top: 5},
		// a notification far off: on its own
		{Grid: 7, Row: 18, Col: 75, Width: 4, Height: 1, OnScreen: true, Top: 7},
	}
	v.fitScreen(rects, [2]int32{0, 40}, 24)
	// too wide: the left edge shows, everything moved alike
	eq(t, rects[0].Col, 0)
	eq(t, rects[1].Col, 2)
	eq(t, rects[2].Col, 0)
	eq(t, rects[3].Col, 39)
	for k, row := range []int32{2, 3, 4, 4} {
		eq(t, rects[k].Row, row)
	}
	// kept inside on its own: 75 - 40, then left to fit
	eq(t, rects[4].Col, 35)
	eq(t, rects[4].Row, 18)
}

func TestScrollbarStaysOnMenu(t *testing.T) {
	v := NewPaneView(30, 10)
	rects := []Rect{
		// past the right edge
		{Grid: 3, Row: 1, Col: 25, Width: 10, Height: 4, OnScreen: true, Top: 3},
		{Grid: 4, Row: 1, Col: 35, Width: 1, Height: 4, OnScreen: true, Top: 3},
	}
	v.fitScreen(rects, [2]int32{0, 0}, 10)
	eq(t, rects[0].Col, 19)
	eq(t, rects[1].Col, 29)
}

func TestFrameFloatsDrawsGroup(t *testing.T) {
	gs := NewGrids()
	gs.grids.put(3, resized(6, 2))
	gs.grids.put(4, resized(3, 1))
	gs.Get(3).Cells[0] = NewCell("a", 0)
	gs.Get(4).Cells[0] = NewCell("b", 0)
	gs.CursorGrid = 4
	gs.Get(4).CursorCol = 1
	placed := []Placed{
		{Grid: 3, Row: 10, Col: 20, ZIndex: 50},
		{Grid: 4, Row: 11, Col: 21, ZIndex: 51, CompIndex: 1},
	}
	v := NewPaneView(8, 4)
	var out bytes.Buffer
	drew, err := v.FrameFloats(gs, placed, [2]int32{10, 20}, &out)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, drew, true)
	eq(t, bytes.Contains(out.Bytes(), []byte("a")), true)
	eq(t, *v.DrawnCursor(), Cursor{Row: 1, Col: 2})
	eq(t, len(v.DrawnFloats()), 2)
	// nothing changed: nothing drawn
	gs.Get(3).ClearDirty()
	gs.Get(4).ClearDirty()
	drew, _ = v.FrameFloats(gs, placed, [2]int32{10, 20}, &out)
	eq(t, drew, false)
}
