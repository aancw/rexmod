package screen

// Scroll: rows Top..Bot moved up by Rows, down if negative.
type Scroll struct {
	Top, Bot uint32
	Rows     int32
}

// MaxCellText: longer grapheme clusters are stored as U+FFFD.
const MaxCellText = 31

// Cell is one grid cell. Text is one grapheme cluster, empty for the right
// half of a wide char. A blank cell's Text is " " (use [BlankCell]); the
// zero Cell is a wide char's right half.
type Cell struct {
	Text string
	Hl   uint32
}

// BlankCell is a space in highlight hl.
func BlankCell(hl uint32) Cell { return Cell{Text: " ", Hl: hl} }

// NewCell is text in highlight hl, U+FFFD if too long.
func NewCell(text string, hl uint32) Cell {
	if len(text) > MaxCellText {
		text = "�"
	}
	return Cell{Text: text, Hl: hl}
}

const maxScrolls = 8

// Grid is one of Neovim's grids: its cells, dirty rows, cursor and pending
// scrolls. Grids routes redraw events here by grid id.
type Grid struct {
	Width, Height uint32
	// Cells is row-major, Width*Height cells.
	Cells []Cell
	// Dirty marks rows changed since the renderer last cleared them.
	Dirty                []bool
	CursorRow, CursorCol uint32
	// full-width scrolls since the last frame, for the terminal to repeat
	scrolls []Scroll
}

// PendingScrolls are the scrolls to repeat before drawing the dirty rows.
func (g *Grid) PendingScrolls() []Scroll { return g.scrolls }

// ClearScrolls forgets the pending scrolls.
func (g *Grid) ClearScrolls() { g.scrolls = g.scrolls[:0] }

// Row is row r's cells.
func (g *Grid) Row(r uint32) []Cell {
	return g.Cells[r*g.Width : (r+1)*g.Width]
}

// DirtyCount is how many rows are dirty.
func (g *Grid) DirtyCount() int {
	n := 0
	for _, d := range g.Dirty {
		if d {
			n++
		}
	}
	return n
}

// SetAllDirty marks every row dirty.
func (g *Grid) SetAllDirty() { setAll(g.Dirty, true) }

// ClearDirty marks every row clean.
func (g *Grid) ClearDirty() { setAll(g.Dirty, false) }

func setAll(b []bool, v bool) {
	for i := range b {
		b[i] = v
	}
}

// Resize to w by h, keeping the cells that still fit; the rest are blank.
// Every row is dirty: Neovim only redraws a resized window's rows that
// changed.
func (g *Grid) Resize(w, h uint32) {
	cells := make([]Cell, int(w)*int(h))
	for i := range cells {
		cells[i] = BlankCell(0)
	}
	keep := min(w, g.Width)
	for r := uint32(0); r < min(h, g.Height); r++ {
		copy(cells[r*w:r*w+keep], g.Cells[r*g.Width:r*g.Width+keep])
	}
	g.Cells = cells
	g.Width, g.Height = w, h
	g.Dirty = make([]bool, h)
	g.SetAllDirty()
	g.scrolls = g.scrolls[:0]
	g.CursorRow = min(g.CursorRow, satSub(h, 1))
	g.CursorCol = min(g.CursorCol, satSub(w, 1))
}

func satSub(a, b uint32) uint32 {
	if a < b {
		return 0
	}
	return a - b
}

// Clear blanks every cell.
func (g *Grid) Clear() {
	for i := range g.Cells {
		g.Cells[i] = BlankCell(0)
	}
	g.SetAllDirty()
	g.scrolls = g.scrolls[:0]
}

// line applies grid_line's cells, [text, hl_id?, repeat?], from column col.
func (g *Grid) line(cells any, r, col uint32) error {
	if r >= g.Height {
		return ErrBadMessage
	}
	dst := g.Row(r)
	c := col
	// an absent hl_id means the previous cell's
	var hl uint32
	list, err := expectArray(cells)
	if err != nil {
		return err
	}
	for _, item := range list {
		parts, err := expectArray(item)
		if err != nil {
			return err
		}
		if len(parts) == 0 {
			return ErrBadMessage
		}
		text, err := expectStr(parts[0])
		if err != nil {
			return err
		}
		if len(parts) >= 2 {
			if hl, err = expectU32(parts[1]); err != nil {
				return err
			}
		}
		repeat := uint32(1)
		if len(parts) >= 3 {
			if repeat, err = expectU32(parts[2]); err != nil {
				return err
			}
		}
		cell := NewCell(text, hl)
		for range repeat {
			if c >= g.Width {
				return ErrBadMessage
			}
			dst[c] = cell
			c++
		}
	}
	g.Dirty[r] = true
	return nil
}

// Scroll moves rows top..bot, columns left..right, up by rows (down if
// negative). The rows left behind are refilled by grid_line events.
func (g *Grid) Scroll(top, bot, left, right uint32, rows int64) error {
	if top > bot || bot > g.Height || left > right || right > g.Width {
		return ErrBadMessage
	}
	if rows == 0 {
		return nil
	}
	abs := rows
	if abs < 0 {
		abs = -abs
	}
	n := uint32(min(abs, int64(bot-top)))
	w := right - left
	if rows > 0 {
		// up: copy top to bottom, so no source row is overwritten first
		for r := top; r+n < bot; r++ {
			g.copyRow(r+n, r, left, w)
		}
	} else {
		// down: copy bottom to top, for the same reason
		for r := bot; r > top+n; {
			r--
			g.copyRow(r-n, r, left, w)
		}
	}

	// a split window's scroll can't be repeated, so redraw its rows
	full := left == 0 && right == g.Width
	if !full || n == bot-top || len(g.scrolls) == maxScrolls {
		for r := top; r < bot; r++ {
			g.Dirty[r] = true
		}
		return nil
	}
	sr := int32(n)
	if rows < 0 {
		sr = -sr
	}
	g.scrolls = append(g.scrolls, Scroll{Top: top, Bot: bot, Rows: sr})
	// dirty bits move with their rows; only the rows left behind are new
	if rows > 0 {
		for r := top; r < bot-n; r++ {
			g.Dirty[r] = g.Dirty[r+n]
		}
		for r := bot - n; r < bot; r++ {
			g.Dirty[r] = true
		}
	} else {
		for r := bot; r > top+n; {
			r--
			g.Dirty[r] = g.Dirty[r-n]
		}
		for r := top; r < top+n; r++ {
			g.Dirty[r] = true
		}
	}
	return nil
}

func (g *Grid) copyRow(from, to, left, w uint32) {
	copy(g.Cells[to*g.Width+left:to*g.Width+left+w], g.Cells[from*g.Width+left:from*g.Width+left+w])
}

// RowText is row r as text, wide-char halves dropped.
func (g *Grid) RowText(r uint32) string {
	var b []byte
	for _, c := range g.Row(r) {
		b = append(b, c.Text...)
	}
	return string(b)
}
