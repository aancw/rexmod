package screen

// Drawing a grid's dirty rows to a terminal as escape sequences. Follows
// Neovim's own TUI (tui_flush in src/nvim/tui/tui.c): trailing blanks are
// cleared with EL instead of printed, and scrolls are repeated with a
// scroll region (DECSTBM) instead of redrawing every moved row.

import (
	"fmt"
	"io"
	"strconv"
)

// Cursor is a terminal cell.
type Cursor struct{ Row, Col uint32 }

// Pen is the SGR the terminal has. The zero Pen is unknown.
type Pen struct {
	hl    uint32
	known bool
}

// ew writes to w until the first error, then remembers it.
type ew struct {
	w   io.Writer
	err error
}

func (e *ew) s(str string) {
	if e.err == nil {
		_, e.err = io.WriteString(e.w, str)
	}
}

func (e *ew) f(format string, a ...any) {
	if e.err == nil {
		_, e.err = fmt.Fprintf(e.w, format, a...)
	}
}

// Frame draws one frame of g filling the terminal: the dirty rows, then the
// cursor, inside synchronized output so the terminal shows it all at once.
// Clears g's dirty rows.
func Frame(g *Grid, style *Style, w io.Writer) error {
	e := &ew{w: w}
	begin(e)
	replayScrolls(g, e)
	var cur Pen
	for r, d := range g.Dirty {
		if d {
			drawRow(g.Row(uint32(r)), style, uint32(r), g.Width, e, &cur)
		}
	}
	g.ClearDirty()
	end(style, &Cursor{g.CursorRow, g.CursorCol}, e)
	return e.err
}

// Begin starts a frame: synchronized output on, cursor hidden while drawing.
func Begin(w io.Writer) error { e := &ew{w: w}; begin(e); return e.err }

func begin(e *ew) { e.s("\x1b[?2026h\x1b[?25l") }

// ReplayScrolls repeats g's pending scrolls with scroll regions, then
// forgets them. The grid must sit at the terminal's top left.
func ReplayScrolls(g *Grid, w io.Writer) error {
	e := &ew{w: w}
	replayScrolls(g, e)
	return e.err
}

func replayScrolls(g *Grid, e *ew) {
	if len(g.scrolls) == 0 {
		return
	}
	// SU and SD fill with the current background
	e.s("\x1b[0m")
	for _, s := range g.scrolls {
		e.f("\x1b[%d;%dr", s.Top+1, s.Bot)
		if s.Rows > 0 {
			e.f("\x1b[%dS", s.Rows)
		} else {
			e.f("\x1b[%dT", -s.Rows)
		}
	}
	e.s("\x1b[r")
	g.scrolls = g.scrolls[:0]
}

// End ends a frame: the cursor where it belongs, or left hidden if nil.
func End(style *Style, cursor *Cursor, w io.Writer) error {
	e := &ew{w: w}
	end(style, cursor, e)
	return e.err
}

func end(style *Style, cursor *Cursor, e *ew) {
	if cursor != nil {
		if m, ok := style.CursorMode(); ok {
			writeCursorStyle(m, e)
		}
		e.f("\x1b[0m\x1b[%d;%dH", cursor.Row+1, cursor.Col+1)
		if !style.Busy {
			e.s("\x1b[?25h")
		}
	} else {
		e.s("\x1b[0m")
	}
	e.s("\x1b[?2026l")
}

// writeCursorStyle: DECSCUSR 1 block, 3 underline, 5 bar; one more for steady.
func writeCursorStyle(m CursorMode, e *ew) {
	shape := 1
	switch m.Shape {
	case CursorHorizontal:
		shape = 3
	case CursorVertical:
		shape = 5
	}
	if !m.Blink {
		shape++
	}
	e.f("\x1b[%d q", shape)
}

// DrawRow draws row at terminal row r, cut at cols columns. pen is the SGR
// the terminal has.
func DrawRow(row []Cell, style *Style, r, cols uint32, w io.Writer, pen *Pen) error {
	e := &ew{w: w}
	drawRow(row, style, r, cols, e, pen)
	return e.err
}

func drawRow(row []Cell, style *Style, r, cols uint32, e *ew, pen *Pen) {
	cells := row[:min(len(row), int(cols))]
	e.f("\x1b[%d;1H", r+1)
	if len(cells) == 0 {
		clearToEnd(style, e, pen)
		return
	}

	// trailing blanks in the last cell's highlight are cleared, not printed
	clearHl := cells[len(cells)-1].Hl
	clearCol := len(cells)
	if canClear(style, clearHl) {
		for clearCol > 0 && isBlank(cells[clearCol-1], clearHl) {
			clearCol--
		}
	}

	for c, cell := range cells[:clearCol] {
		// the right half of a wide char: the terminal already moved past it
		if cell.Text == "" {
			continue
		}
		setHl(style, cell.Hl, e, pen)
		// a wide char cut in half at the edge would wrap
		cut := c+1 == len(cells) && len(cells) < len(row) && row[c+1].Text == ""
		if cut {
			e.s(" ")
		} else {
			e.s(cell.Text)
		}
	}
	if clearCol < len(cells) {
		// EL fills with the current background
		setHl(style, clearHl, e, pen)
		e.s("\x1b[K")
	} else if uint32(len(cells)) < cols {
		// past a grid narrower than the terminal
		clearToEnd(style, e, pen)
	}
}

// ClearRow blanks terminal row r in the default colors.
func ClearRow(style *Style, r uint32, w io.Writer, pen *Pen) error {
	e := &ew{w: w}
	clearRow(style, r, e, pen)
	return e.err
}

func clearRow(style *Style, r uint32, e *ew, pen *Pen) {
	e.f("\x1b[%d;1H", r+1)
	clearToEnd(style, e, pen)
}

func clearToEnd(style *Style, e *ew, pen *Pen) {
	setHl(style, 0, e, pen)
	e.s("\x1b[K")
}

// canClear: EL keeps only the background, so nothing else may show on a blank.
func canClear(style *Style, hl uint32) bool {
	a := style.Attr(hl)
	return !a.Reverse && !a.Strikethrough && !a.Overline && a.Underline == UnderlineNone
}

func isBlank(c Cell, hl uint32) bool { return c.Hl == hl && c.Text == " " }

// SetHl switches the terminal to highlight hl, if pen doesn't have it.
func SetHl(style *Style, hl uint32, w io.Writer, pen *Pen) error {
	e := &ew{w: w}
	setHl(style, hl, e, pen)
	return e.err
}

func setHl(style *Style, hl uint32, e *ew, pen *Pen) {
	if pen.known && pen.hl == hl {
		return
	}
	*pen = Pen{hl: hl, known: true}
	writeSgr(style.Attr(hl), style, e)
}

// WriteSgr writes one SGR sequence that resets, then sets everything in
// attr. Colors attr leaves unset use its palette index, then the style's
// defaults; if those are unset too, the terminal's own (from ext_termcolors).
func WriteSgr(attr Attr, style *Style, w io.Writer) error {
	e := &ew{w: w}
	writeSgr(attr, style, e)
	return e.err
}

func writeSgr(a Attr, style *Style, e *ew) {
	b := []byte("\x1b[0")
	flag := func(on bool, s string) {
		if on {
			b = append(b, s...)
		}
	}
	flag(a.Bold, ";1")
	flag(a.Dim, ";2")
	flag(a.Italic, ";3")
	flag(a.Blink, ";5")
	flag(a.Reverse, ";7")
	flag(a.Conceal, ";8")
	flag(a.Strikethrough, ";9")
	flag(a.Overline, ";53")
	switch a.Underline {
	case UnderlineSingle:
		b = append(b, ";4"...)
	case UnderlineDouble:
		b = append(b, ";4:2"...)
	case UnderlineCurl:
		b = append(b, ";4:3"...)
	case UnderlineDotted:
		b = append(b, ";4:4"...)
	case UnderlineDashed:
		b = append(b, ";4:5"...)
	}
	b = appendLayer(b, 30, a.Fg, a.FgIdx, style.DefaultFg)
	b = appendLayer(b, 40, a.Bg, a.BgIdx, style.DefaultBg)
	if a.Underline != UnderlineNone {
		b = appendColor(b, 58, orColor(a.Sp, style.DefaultSp))
	}
	b = append(b, 'm')
	e.s(string(b))
}

// appendColor: ;38;2;r;g;b and friends; nothing for unset, which the reset covers.
func appendColor(b []byte, prefix int, c Color) []byte {
	if !c.Set {
		return b
	}
	return fmt.Appendf(b, ";%d;2;%d;%d;%d", prefix, c.RGB>>16, (c.RGB>>8)&0xff, c.RGB&0xff)
}

// appendLayer: text (base 30) or background (40) color: RGB, else the
// palette index, else the default.
func appendLayer(b []byte, base int, rgb Color, idx PaletteIndex, def Color) []byte {
	if !rgb.Set && idx.Set {
		return appendIndexed(b, base, idx.I)
	}
	return appendColor(b, base+8, orColor(rgb, def))
}

// appendIndexed: palette color i with base 30 (fg) or 40 (bg): ;34, ;94 for
// the bright half, ;38;5;i past 15.
func appendIndexed(b []byte, base int, i uint8) []byte {
	switch {
	case i < 8:
		return strconv.AppendInt(append(b, ';'), int64(base+int(i)), 10)
	case i < 16:
		return strconv.AppendInt(append(b, ';'), int64(base+60+int(i)-8), 10)
	}
	return fmt.Appendf(b, ";%d;5;%d", base+8, i)
}
