package screen

import (
	"bytes"
	"testing"
)

type fixture struct {
	g *Grid
	s Style
}

func newFixture(w, h uint32) *fixture {
	f := &fixture{g: resized(w, h)}
	f.s.Attrs = []Attr{{}, {Bold: true, Fg: RGB(0xff0000)}, {Bg: RGB(0x0000ff)}}
	return f
}

func (f *fixture) expectFrame(t *testing.T, want string) {
	t.Helper()
	var b bytes.Buffer
	if err := Frame(f.g, &f.s, &b); err != nil {
		t.Fatal(err)
	}
	eq(t, b.String(), want)
}

const beginS = "\x1b[?2026h\x1b[?25l"
const endS = "\x1b[?25h\x1b[?2026l"

func TestBlankRowsJustCleared(t *testing.T) {
	f := newFixture(4, 2)
	f.expectFrame(t, beginS+"\x1b[1;1H\x1b[0m\x1b[K"+"\x1b[2;1H\x1b[K"+"\x1b[0m\x1b[1;1H"+endS)
	eq(t, f.g.DirtyCount(), 0)
}

func TestOnlyDirtyRows(t *testing.T) {
	f := newFixture(5, 3)
	g := f.g
	g.ClearDirty()
	row := g.Cells[5:10]
	row[0] = NewCell("h", 1)
	row[1] = NewCell("i", 1)
	row[2] = NewCell("!", 0)
	g.Dirty[1] = true
	g.CursorRow, g.CursorCol = 1, 3
	f.expectFrame(t, beginS+
		"\x1b[2;1H"+"\x1b[0;1;38;2;255;0;0mhi"+"\x1b[0m!"+"\x1b[K"+
		"\x1b[0m\x1b[2;4H"+endS)
}

func TestColoredTrailingBlanks(t *testing.T) {
	f := newFixture(4, 1)
	f.g.Cells[0] = NewCell("x", 0)
	for k := 1; k < 4; k++ {
		f.g.Cells[k] = NewCell(" ", 2)
	}
	f.expectFrame(t, beginS+
		"\x1b[1;1H\x1b[0mx"+"\x1b[0;48;2;0;0;255m\x1b[K"+
		"\x1b[0m\x1b[1;1H"+endS)
}

func TestWideCharsSkipRightHalf(t *testing.T) {
	f := newFixture(4, 1)
	f.g.Cells[0] = NewCell("界", 0)
	f.g.Cells[1] = NewCell("", 0)
	f.g.Cells[2] = NewCell("a", 0)
	f.expectFrame(t, beginS+"\x1b[1;1H\x1b[0m界a\x1b[K"+"\x1b[0m\x1b[1;1H"+endS)
}

func TestScrollsGoFirst(t *testing.T) {
	f := newFixture(2, 4)
	f.g.ClearDirty()
	f.g.scrolls = []Scroll{{Top: 1, Bot: 4, Rows: 2}, {Top: 0, Bot: 4, Rows: -1}}
	f.expectFrame(t, beginS+
		"\x1b[0m\x1b[2;4r\x1b[2S\x1b[1;4r\x1b[1T\x1b[r"+
		"\x1b[0m\x1b[1;1H"+endS)
	eq(t, len(f.g.PendingScrolls()), 0)
}

func TestCursorStyleAndBusy(t *testing.T) {
	f := newFixture(2, 1)
	f.g.ClearDirty()
	f.s.CursorModes = []CursorMode{{Shape: CursorVertical, Blink: true}}
	f.s.CursorStyleEnabled = true
	f.s.Busy = true
	f.expectFrame(t, beginS+"\x1b[5 q\x1b[0m\x1b[1;1H\x1b[?2026l")
}

func TestRowsCutAtTerminalWidth(t *testing.T) {
	f := newFixture(4, 1)
	f.g.Cells[0] = NewCell("a", 1)
	f.g.Cells[1] = NewCell("b", 1)
	f.g.Cells[2] = NewCell("界", 1)
	f.g.Cells[3] = NewCell("", 1)
	var b bytes.Buffer
	var pen Pen
	// the wide char would straddle the edge, so a blank stands in
	if err := DrawRow(f.g.Row(0), &f.s, 0, 3, &b, &pen); err != nil {
		t.Fatal(err)
	}
	eq(t, b.String(), "\x1b[1;1H\x1b[0;1;38;2;255;0;0mab ")
	b.Reset()
	DrawRow(f.g.Row(0), &f.s, 0, 6, &b, &pen)
	eq(t, b.String(), "\x1b[1;1Hab界\x1b[0m\x1b[K")
}

func TestSgrPaletteIndices(t *testing.T) {
	s := Style{DefaultFg: RGB(0x010203)}
	var b bytes.Buffer
	// RGB wins over the index; an index stands in for the default
	WriteSgr(Attr{Fg: RGB(0xff0000), FgIdx: Idx(4), BgIdx: Idx(12)}, &s, &b)
	WriteSgr(Attr{FgIdx: Idx(3), BgIdx: Idx(200)}, &s, &b)
	eq(t, b.String(), "\x1b[0;38;2;255;0;0;104m"+"\x1b[0;33;48;5;200m")
}

func TestSgrDefaults(t *testing.T) {
	s := Style{DefaultFg: RGB(0x010203)}
	var b bytes.Buffer
	WriteSgr(Attr{Italic: true, Underline: UnderlineCurl, Sp: RGB(0x00ff00)}, &s, &b)
	eq(t, b.String(), "\x1b[0;3;4:3;38;2;1;2;3;58;2;0;255;0m")
}
