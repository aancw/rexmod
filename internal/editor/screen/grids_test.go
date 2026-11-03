package screen

import (
	"strings"
	"testing"
)

func TestEventsGoToTheirGrids(t *testing.T) {
	gs := NewGrids()
	flushed := apply(t, gs,
		iev("grid_resize", 1, 4, 3),
		iev("grid_resize", 2, 3, 2),
		iev("grid_resize", 4, 4, 3),
		lineEvent(2, 1, 0, "x"),
		lineEvent(4, 0, 1, "m"),
		winEvent("win_external_pos", 2, 1000),
		winEvent("win_pos", 3, 1001),
		iev("msg_set_pos", 4, 2),
		iev("grid_cursor_goto", 2, 1, 2),
		iev("flush"),
	)
	eq(t, flushed, true)
	eq(t, gs.Get(2).RowText(0), "   ")
	eq(t, gs.Get(2).RowText(1), "x  ")
	eq(t, gs.Get(1).RowText(1), "    ")
	eq(t, gs.Get(4).RowText(0), " m  ")
	id, ok := gs.GridOf(1000)
	eq(t, ok, true)
	eq(t, id, 2)
	w, _ := gs.WinOn(2)
	eq(t, w.Kind, WinExternal)
	w, _ = gs.WinOn(3)
	eq(t, w.Kind, WinOrdinary)
	eq(t, gs.CursorGrid, 2)
	eq(t, gs.Get(2).CursorRow, 1)
	// grid 1 is 3 rows; messages from row 2 down
	eq(t, gs.MsgRows(), 1)

	apply(t, gs,
		iev("win_hide", 3),
		iev("win_close", 2),
		iev("grid_destroy", 2),
		// a line for a grid that's gone is skipped
		lineEvent(2, 0, 0, "y"),
	)
	if gs.Get(2) != nil {
		t.Fatal("grid 2 still there")
	}
	_, ok = gs.GridOf(1000)
	eq(t, ok, false)
	w, _ = gs.WinOn(3)
	eq(t, w.Kind, WinHidden)
}

func TestResizeLineCursorFlush(t *testing.T) {
	gs := NewGrids()
	flushed := apply(t, gs,
		iev("grid_resize", 1, 6, 2),
		ev("grid_line", i(1), i(0), i(0), []any{
			[]any{"h", i(3)},
			[]any{"e"},
			[]any{"l", i(3), i(2)},
			[]any{"o", i(4)},
		}, false),
		iev("grid_cursor_goto", 1, 1, 2),
		iev("flush"),
	)
	eq(t, flushed, true)
	g := gs.Get(1)
	eq(t, g.RowText(0), "hello ")
	eq(t, g.RowText(1), "      ")
	// "e" carries "h"'s hl; the untouched last cell keeps 0
	for k, hl := range []uint32{3, 3, 3, 3, 4, 0} {
		eq(t, g.Row(0)[k].Hl, hl)
	}
	eq(t, g.CursorRow, 1)
	eq(t, g.CursorCol, 2)
	eq(t, g.Dirty[0] && g.Dirty[1], true)
}

func TestWideCharsAndLongClusters(t *testing.T) {
	gs := NewGrids()
	apply(t, gs,
		iev("grid_resize", 1, 4, 1),
		ev("grid_line", i(1), i(0), i(0), []any{
			[]any{"界", i(0)},
			[]any{""},
			[]any{strings.Repeat("x", MaxCellText+2)},
		}, false),
	)
	row := gs.Get(1).Row(0)
	eq(t, row[0].Text, "界")
	eq(t, row[1].Text, "")
	eq(t, row[2].Text, "�")
}

func TestHlAttrDefineAndDefaultColors(t *testing.T) {
	gs := NewGrids()
	apply(t, gs,
		iev("default_colors_set", 0xffffff, 0, -1, 0, 0),
		ev("hl_attr_define", i(5), tmap{
			{"foreground", i(0x112233)},
			{"bold", true},
			{"undercurl", true},
			{"url", "https://neovim.io"},
		}, tmap{}, []any{}),
	)
	s := &gs.Style
	eq(t, s.DefaultFg, RGB(0xffffff))
	eq(t, s.DefaultSp, Color{})
	a := s.Attrs[5]
	eq(t, a.Fg, RGB(0x112233))
	eq(t, a.Bg, Color{})
	eq(t, a.Bold && !a.Italic, true)
	eq(t, a.Underline, UnderlineCurl)
	eq(t, len(s.Attrs), 6)
}

func TestHlAttrDefinePaletteOnlyWithoutRGB(t *testing.T) {
	define := func(id int64, bg *int64) any {
		rgb := tmap{}
		if bg != nil {
			rgb = tmap{{"background", *bg}}
		}
		return ev("hl_attr_define", id, rgb, tmap{{"foreground", i(4)}, {"background", i(9)}}, []any{})
	}
	gs := NewGrids()
	bg := int64(0x112233)
	apply(t, gs, define(1, nil), define(2, &bg))
	only := gs.Style.Attrs[1]
	eq(t, only.FgIdx, Idx(4))
	eq(t, only.BgIdx, Idx(9))
	// like the default Visual: an RGB background, and ctermfg only for
	// 256-color terminals
	mixed := gs.Style.Attrs[2]
	eq(t, mixed.FgIdx, PaletteIndex{})
	eq(t, mixed.BgIdx, PaletteIndex{})
	eq(t, mixed.Bg, RGB(0x112233))
}

func TestCursorModesAndBusy(t *testing.T) {
	gs := NewGrids()
	apply(t, gs,
		ev("mode_info_set", true, []any{
			tmap{{"cursor_shape", "block"}},
			tmap{{"cursor_shape", "vertical"}, {"blinkon", i(500)}, {"blinkoff", i(500)}, {"name", "insert"}},
		}),
		ev("mode_change", "insert", i(1)),
		iev("busy_start"),
	)
	s := &gs.Style
	m, ok := s.CursorMode()
	eq(t, ok, true)
	eq(t, m, CursorMode{Shape: CursorVertical, Blink: true})
	eq(t, s.Busy, true)
	s.ModeIdx = 0
	m, _ = s.CursorMode()
	eq(t, m, CursorMode{})
	s.CursorStyleEnabled = false
	_, ok = s.CursorMode()
	eq(t, ok, false)
}

func TestUnknownEventsSkipped(t *testing.T) {
	gs := NewGrids()
	flushed := apply(t, gs, []any{"option_set", []any{"emoji", true}, []any{"linespace", i(0)}})
	eq(t, flushed, false)
}

func TestFloatsPlacedOverGridUnder(t *testing.T) {
	gs := NewGrids()
	apply(t, gs,
		iev("grid_resize", 2, 20, 10),
		winEvent("win_external_pos", 2, 1000),
		iev("grid_resize", 3, 4, 2),
		iev("grid_resize", 4, 3, 1),
		iev("grid_resize", 5, 5, 2),
		// its bottom right corner at 6.5, 10 of grid 2
		floatEvent(3, "SE", 2, 6.5, 10, 50, nil),
		// over the float, so over grid 2 too; drawn above it
		floatEvent(4, "NW", 3, 1, 1, 40, &[2]int64{0, 0}),
		// on the screen: where Neovim put it
		floatEvent(5, "NW", 1, 30, 30, 50, &[2]int64{7, 2}),
		iev("flush"),
	)
	over := gs.FloatsOver(2, false, nil)
	eq(t, len(over), 2)
	eq(t, over[0], Placed{Grid: 4, Row: 5, Col: 7, ZIndex: 40})
	eq(t, over[1], Placed{Grid: 3, Row: 4, Col: 6, ZIndex: 50})
	all := gs.FloatsOver(2, true, nil)
	eq(t, len(all), 3)
	eq(t, all[2], Placed{Grid: 5, Row: 7, Col: 2, ZIndex: 50, OnScreen: true, Top: 5})
	// left out with the editor float it hangs from
	eq(t, len(gs.FloatsOver(2, true, []uint32{5})), 2)
}

func TestFloatsOverEditorFloatHangFromIt(t *testing.T) {
	gs := NewGrids()
	apply(t, gs,
		iev("grid_resize", 3, 20, 8),
		iev("grid_resize", 4, 10, 1),
		iev("grid_resize", 5, 4, 2),
		// a box on the screen, an input inside it, the pair on a third
		floatEvent(3, "NW", 1, 2, 5, 50, &[2]int64{2, 5}),
		floatEvent(4, "NW", 3, 1, 1, 51, nil),
		floatEvent(5, "NW", 4, 0, 2, 52, nil),
	)
	for _, c := range []struct {
		id   uint32
		want Place
	}{
		{3, Place{Root: 1, Row: 2, Col: 5, Top: 3}},
		{4, Place{Root: 1, Row: 3, Col: 6, Top: 3}},
		{5, Place{Root: 1, Row: 3, Col: 8, Top: 3}},
	} {
		p, ok := gs.Place(c.id)
		eq(t, ok, true)
		eq(t, p, c.want)
	}
	eq(t, len(gs.FloatsOver(1, true, []uint32{3})), 0)
}

func TestBadMessages(t *testing.T) {
	for _, e := range []any{
		iev("grid_resize", 1, 2),
		[]any{},
		ev("grid_line", i(1), i(5), i(0), []any{}, false),
		ev("win_pos", i(2), "nope"),
	} {
		gs := NewGrids()
		apply(t, gs, iev("grid_resize", 1, 2, 2))
		if _, err := gs.ApplyRedraw([]any{e}); err != ErrBadMessage {
			t.Fatalf("%v: got %v", e, err)
		}
	}
}
