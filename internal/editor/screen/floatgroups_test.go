package screen

import (
	"slices"
	"testing"
)

func addFloat(gs *Grids, grid uint32, win int64, w, h uint32, f Float) {
	gs.grids.put(grid, resized(w, h))
	gs.wins.put(grid, Win{Win: win, Kind: WinFloat, Float: f})
}

// fl is a float with the default position changed by set.
func fl(set func(*Float)) Float {
	f := DefaultFloat()
	if set != nil {
		set(&f)
	}
	return f
}

func onScreen(row, col int32) func(*Float) {
	return func(f *Float) { f.Screen, f.HasScreen = [2]int32{row, col}, true }
}

func unfocusable(set func(*Float)) func(*Float) {
	return func(f *Float) { set(f); f.Focusable = false }
}

func TestPickerFloatsAreOneGroup(t *testing.T) {
	gs := NewGrids()
	// a picker as snacks makes it: an unfocusable box, input and list on it
	addFloat(gs, 3, 1002, 60, 20, fl(unfocusable(onScreen(4, 10))))
	addFloat(gs, 4, 1004, 58, 1, fl(func(f *Float) { f.AnchorGrid, f.Row, f.Col = 3, 1, 1 }))
	addFloat(gs, 5, 1005, 58, 16, fl(func(f *Float) { f.AnchorGrid, f.Row, f.Col = 3, 3, 1 }))
	// a notification: unfocusable
	addFloat(gs, 6, 1006, 30, 6, fl(unfocusable(onScreen(0, 100))))
	// a small focusable input: its own group
	addFloat(gs, 7, 1007, 30, 1, fl(onScreen(40, 0)))
	// the popup menu
	addFloat(gs, 8, -1, 30, 10, fl(onScreen(40, 40)))
	// a focusable float nobody went into
	addFloat(gs, 9, 1009, 20, 4, fl(onScreen(30, 100)))

	groups := FindGroups(gs, []int64{1004, 1007})
	eq(t, len(groups), 2)
	if !slices.Equal(groups[1].TopGrids(), []uint32{7}) {
		t.Fatal(groups[1].TopGrids())
	}
	g := groups[0]
	if !slices.Equal(g.TopGrids(), []uint32{3}) {
		t.Fatal(g.TopGrids())
	}
	eq(t, g.Key, 1002)
	eq(t, g.Row, 4)
	eq(t, g.Col, 10)
	eq(t, g.Width, 60)
	eq(t, g.Height, 20)
	eq(t, len(GroupMembers(gs, &g)), 3)
}

func TestTelescopeFloatsAreOneGroup(t *testing.T) {
	gs := NewGrids()
	// prompt over results, preview to the right with a gap of a cell
	addFloat(gs, 3, 1010, 40, 3, fl(onScreen(2, 2)))
	addFloat(gs, 4, 1011, 40, 20, fl(onScreen(5, 2)))
	addFloat(gs, 5, 1012, 40, 23, fl(onScreen(2, 43)))
	// only the prompt takes focus
	groups := FindGroups(gs, []int64{1010})
	eq(t, len(groups), 1)
	eq(t, len(groups[0].TopGrids()), 3)
	eq(t, groups[0].Key, 1010)
	eq(t, groups[0].Width, 81)
	eq(t, groups[0].Height, 23)
}

func TestCompletionMenuStaysInPane(t *testing.T) {
	gs := NewGrids()
	// the command line popup, as noice makes it
	addFloat(gs, 3, 1020, 62, 3, fl(unfocusable(onScreen(4, 40))))
	// blink's menu right under it: focusable, never entered
	addFloat(gs, 4, 1021, 30, 8, fl(onScreen(7, 41)))
	// and its scrollbar, on the menu
	addFloat(gs, 5, 1022, 1, 3, fl(func(f *Float) { f.AnchorGrid, f.Col = 4, 29 }))
	eq(t, len(FindGroups(gs, nil)), 0)
	// a float elsewhere was entered: still not these
	eq(t, len(FindGroups(gs, []int64{1000})), 0)
}
