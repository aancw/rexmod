package screen

// What one pane's terminal shows: a window's grid from the top left, the
// floats over it and, in the focused pane, the screen's floats and the
// message grid over the bottom rows. Remembers what it drew so each frame
// sends only what changed.
//
// Floats are drawn into their window's pane, except groups shown in Rex
// floating panes, which FrameFloats draws on their own.

import (
	"io"
	"slices"
)

// PaneView is one pane's terminal. Make it with NewPaneView.
type PaneView struct {
	// Cols, Rows is the terminal's size.
	Cols, Rows uint32
	// Full: draw everything on the next frame.
	Full bool
	// the grid drawn last, and its size
	drawnGrid uint32
	drawnSize [2]uint32
	// message rows drawn over the bottom last frame
	drawnMsg    uint32
	drawnCursor *Cursor
	// the floats drawn last frame
	drawnFloats []Rect
	line        []Cell
}

// Opts is how a pane draws the screen's floats and messages.
type Opts struct {
	// Focused draws the messages and the screen's floats.
	Focused bool
	// Origin is the pane's top left on grid 1, which spans the Rex window:
	// screen floats move by it, then into the pane.
	Origin [2]int32
	// Skip are editor floats shown elsewhere, with every float hanging
	// from them.
	Skip []uint32
}

// Rect is a float as drawn, in terminal cells.
type Rect struct {
	Grid          uint32
	Row, Col      int32
	Width, Height uint32
	OnScreen      bool
	OnMsg         bool
	// Top is the editor float it hangs from, itself included; 0 if none.
	Top uint32
}

func clamp64(v, lo, hi int64) int64 { return max(lo, min(v, hi)) }

func (r Rect) rows(limit uint32) (uint32, uint32) {
	top := clamp64(int64(r.Row), 0, int64(limit))
	bot := clamp64(int64(r.Row)+int64(r.Height), 0, int64(limit))
	return uint32(top), uint32(bot)
}

const (
	// widest row floats are drawn into
	maxCols = 1024
	// tallest terminal a floating pane draws into
	maxRows = 512
)

// NewPaneView is a view of a cols by rows terminal, drawn in full first.
func NewPaneView(cols, rows uint32) *PaneView {
	return &PaneView{Cols: cols, Rows: rows, Full: true}
}

// Resize: the terminal changed size; its old contents can't be trusted.
func (v *PaneView) Resize(cols, rows uint32) {
	if cols == v.Cols && rows == v.Rows {
		return
	}
	v.Cols, v.Rows = cols, rows
	v.Full = true
}

// DrawnCursor is where the cursor was left last frame, nil if hidden.
func (v *PaneView) DrawnCursor() *Cursor { return v.drawnCursor }

// DrawnFloats are the floats drawn last frame, lowest first.
func (v *PaneView) DrawnFloats() []Rect { return v.drawnFloats }

func sameCursor(a, b *Cursor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Frame draws a frame if anything changed: grid gridID (0 for none yet)
// and, if focused, the messages. Clears the grid's dirty rows but not the
// message grid's, which the caller clears once every pane is drawn. drew
// is true if it wrote anything.
func (v *PaneView) Frame(grids *Grids, gridID uint32, opts Opts, w io.Writer) (drew bool, err error) {
	e := &ew{w: w}
	focused := opts.Focused
	style := &grids.Style
	style.ClearBlends()
	id := gridID
	var g *Grid
	if id != 0 {
		g = grids.Get(id)
	}
	var size [2]uint32
	if g != nil {
		size = [2]uint32{g.Width, g.Height}
	}
	if id != v.drawnGrid || size != v.drawnSize {
		v.Full = true
	}

	// the message grid's rows that show, from the bottom
	var msgTotal uint32
	if focused {
		msgTotal = grids.MsgRows()
	}
	msgN := min(msgTotal, v.Rows)
	var msg *Grid
	if msgN > 0 {
		msg = grids.Get(grids.MsgGrid)
	}
	msgTop := v.Rows - msgN
	gridRows := min(size[1], msgTop)

	var rects []Rect
	if g != nil {
		rects = v.fitScreen(floatRects(grids, grids.FloatsOver(id, focused, opts.Skip)), opts.Origin, screenRows(grids))
	}
	floatsMoved := !slices.Equal(rects, v.drawnFloats)
	floatsChanged := floatsMoved
	for _, r := range rects {
		f := grids.Get(r.Grid)
		if f.DirtyCount() > 0 || len(f.scrolls) > 0 {
			floatsChanged = true
		}
	}

	cur := v.cursorAt(grids, g, id, gridRows, msg, msgTotal, msgN, rects)
	gridChanged := floatsChanged || (g != nil && (g.DirtyCount() > 0 || len(g.scrolls) > 0))
	msgChanged := msgN != v.drawnMsg || (msg != nil && msg.DirtyCount() > 0)
	if !v.Full && !gridChanged && !msgChanged && sameCursor(cur, v.drawnCursor) {
		return false, nil
	}

	begin(e)
	var pen Pen
	oldTop := v.Rows - min(v.drawnMsg, v.Rows)
	switch {
	case v.Full:
		// ED fills with the current background
		setHl(style, 0, e, &pen)
		e.s("\x1b[2J")
		if g != nil {
			g.SetAllDirty()
			g.ClearScrolls()
		}
	case g != nil:
		v.scrolls(g, gridRows, rects, e)
		// rows under floats that moved, changed or went
		if floatsMoved {
			for _, r := range v.drawnFloats {
				markRows(g, r, gridRows)
			}
		}
		for _, r := range rects {
			f := grids.Get(r.Grid)
			if floatsMoved || len(f.scrolls) > 0 {
				markRows(g, r, gridRows)
				continue
			}
			for fr, d := range f.Dirty {
				if !d {
					continue
				}
				tr := int64(r.Row) + int64(fr)
				if tr >= 0 && tr < int64(gridRows) {
					g.Dirty[tr] = true
				}
			}
		}
		// rows the messages covered last frame and no longer do
		for r := min(oldTop, msgTop); r < msgTop; r++ {
			if r < gridRows {
				g.Dirty[r] = true
			} else {
				clearRow(style, r, e, &pen)
			}
		}
	default:
		for r := min(oldTop, msgTop); r < msgTop; r++ {
			clearRow(style, r, e, &pen)
		}
	}
	if g != nil {
		for r, d := range g.Dirty {
			if uint32(r) >= gridRows {
				break
			}
			if !d {
				continue
			}
			shown := v.compose(grids, g.Row(uint32(r)), rects, uint32(r), min(v.Cols, maxCols))
			drawRow(shown, style, uint32(r), v.Cols, e, &pen)
		}
		g.ClearDirty()
	}
	if msg != nil && (v.Full || msgChanged) {
		for i := range msgN {
			drawRow(msg.Row(msgTotal-msgN+i), style, msgTop+i, v.Cols, e, &pen)
		}
	}
	end(style, cur, e)

	v.Full = false
	v.drawnGrid = id
	v.drawnSize = size
	v.drawnMsg = msgN
	v.drawnCursor = cur
	v.drawnFloats = append(v.drawnFloats[:0], rects...)
	return true, e.err
}

func screenRows(grids *Grids) uint32 {
	if g := grids.Get(1); g != nil {
		return g.Height
	}
	return 0
}

// fitScreen moves screen floats from grid 1 into the pane at origin, and
// keeps them inside it where they fit. Floats that go together move by the
// same amount, so they stay lined up: a float and those hanging from it,
// such as a border or a scrollbar, and floats that touch, such as a command
// line and the completion menu under it. Too big to fit, their top left
// shows, where the text starts. Message floats keep to the bottom, as the
// messages do.
func (v *PaneView) fitScreen(rects []Rect, origin [2]int32, screenRows uint32) []Rect {
	for i := range rects {
		r := &rects[i]
		if !r.OnScreen {
			continue
		}
		if r.OnMsg {
			up := int32(screenRows) - int32(v.Rows)
			r.Row = fitInto(r.Row-up, r.Height, v.Rows)
			r.Col = fitInto(r.Col, r.Width, v.Cols)
			continue
		}
		r.Row -= origin[0]
		r.Col -= origin[1]
	}
	// the floats that go together, each named by one of them
	set := make([]int, len(rects))
	for i := range set {
		set[i] = i
	}
	for joined := true; joined; {
		joined = false
		for i, a := range rects {
			for j := i + 1; j < len(rects); j++ {
				b := rects[j]
				if set[i] == set[j] || !movable(a) || !movable(b) || !together(a, b) {
					continue
				}
				from := set[j]
				for k := range set {
					if set[k] == from {
						set[k] = set[i]
					}
				}
				joined = true
			}
		}
	}
	for i, r := range rects {
		if !movable(r) || set[i] != i {
			continue
		}
		// the box around this set
		top, left := int64(r.Row), int64(r.Col)
		bottom, right := int64(r.Row)+int64(r.Height), int64(r.Col)+int64(r.Width)
		for k, m := range rects {
			if set[k] == i {
				top = min(top, int64(m.Row))
				left = min(left, int64(m.Col))
				bottom = max(bottom, int64(m.Row)+int64(m.Height))
				right = max(right, int64(m.Col)+int64(m.Width))
			}
		}
		down := fitStart(top, bottom-top, v.Rows) - top
		across := fitStart(left, right-left, v.Cols) - left
		for k := range rects {
			if set[k] == i {
				rects[k].Row = int32(int64(rects[k].Row) + down)
				rects[k].Col = int32(int64(rects[k].Col) + across)
			}
		}
	}
	return rects
}

// movable: a screen float fitScreen may move with others.
func movable(r Rect) bool { return r.OnScreen && !r.OnMsg }

// together: hang from the same editor float, or overlap or touch.
func together(a, b Rect) bool {
	if a.Top != 0 && a.Top == b.Top {
		return true
	}
	return int64(a.Row) <= int64(b.Row)+int64(b.Height) && int64(b.Row) <= int64(a.Row)+int64(a.Height) &&
		int64(a.Col) <= int64(b.Col)+int64(b.Width) && int64(b.Col) <= int64(a.Col)+int64(a.Width)
}

// fitStart: pos moved back inside 0..limit if len fits there, else to 0.
func fitStart(pos, n int64, limit uint32) int64 {
	if n > int64(limit) {
		return 0
	}
	return clamp64(pos, 0, int64(limit)-n)
}

// fitInto: pos moved back inside 0..limit if len fits there.
func fitInto(pos int32, n, limit uint32) int32 {
	if n > limit {
		return max(min(pos, 0), int32(limit)-int32(n))
	}
	return int32(clamp64(int64(pos), 0, int64(limit-n)))
}

// FrameFloats draws just floats placed, moved up and left by origin, over a
// blank terminal: a group shown in a Rex floating pane. drew is true if it
// wrote anything.
func (v *PaneView) FrameFloats(grids *Grids, placed []Placed, origin [2]int32, w io.Writer) (drew bool, err error) {
	e := &ew{w: w}
	style := &grids.Style
	style.ClearBlends()
	rects := floatRects(grids, placed)
	for i := range rects {
		rects[i].Row -= origin[0]
		rects[i].Col -= origin[1]
	}
	if !slices.Equal(rects, v.drawnFloats) {
		v.Full = true
	}

	rows := min(v.Rows, maxRows)
	dirty := make([]bool, rows)
	anyDirty := false
	for _, r := range rects {
		f := grids.Get(r.Grid)
		top, bot := r.rows(rows)
		if len(f.scrolls) > 0 {
			for i := top; i < bot; i++ {
				dirty[i], anyDirty = true, true
			}
			continue
		}
		for fr, d := range f.Dirty {
			if !d {
				continue
			}
			tr := int64(r.Row) + int64(fr)
			if tr >= 0 && tr < int64(rows) {
				dirty[tr], anyDirty = true, true
			}
		}
	}
	cur := floatCursor(grids, rects, rows, v.Cols)
	if !v.Full && !anyDirty && sameCursor(cur, v.drawnCursor) {
		return false, nil
	}

	begin(e)
	var pen Pen
	if v.Full {
		setHl(style, 0, e, &pen)
		e.s("\x1b[2J")
		setAll(dirty, false)
		for _, r := range rects {
			top, bot := r.rows(rows)
			for i := top; i < bot; i++ {
				dirty[i] = true
			}
		}
	}
	for r, d := range dirty {
		if !d {
			continue
		}
		shown := v.compose(grids, nil, rects, uint32(r), min(v.Cols, maxCols))
		drawRow(shown, style, uint32(r), v.Cols, e, &pen)
	}
	end(style, cur, e)

	v.Full = false
	v.drawnGrid = 0
	v.drawnSize = [2]uint32{}
	v.drawnCursor = cur
	v.drawnFloats = append(v.drawnFloats[:0], rects...)
	return true, e.err
}

// floatCursor: the cursor, if it's on one of the floats and inside the terminal.
func floatCursor(grids *Grids, rects []Rect, rows, cols uint32) *Cursor {
	for _, r := range rects {
		if grids.CursorGrid != r.Grid {
			continue
		}
		f := grids.Get(r.Grid)
		row := int64(r.Row) + int64(f.CursorRow)
		col := int64(r.Col) + int64(f.CursorCol)
		if row < 0 || row >= int64(rows) || col < 0 || col >= int64(cols) {
			return nil
		}
		return &Cursor{uint32(row), uint32(col)}
	}
	return nil
}

// scrolls repeats the grid's scrolls if they stay clear of the messages and
// floats; else redraws the rows they moved.
func (v *PaneView) scrolls(g *Grid, gridRows uint32, rects []Rect, e *ew) {
	for _, s := range g.scrolls {
		if s.Bot > gridRows || underFloat(s, rects, gridRows) {
			// one can't be repeated, so none are: the terminal must match each in turn
			for _, p := range g.scrolls {
				for r := p.Top; r < p.Bot; r++ {
					g.Dirty[r] = true
				}
			}
			g.ClearScrolls()
			return
		}
	}
	replayScrolls(g, e)
}

func underFloat(s Scroll, rects []Rect, gridRows uint32) bool {
	for _, r := range rects {
		top, bot := r.rows(gridRows)
		if top < s.Bot && s.Top < bot {
			return true
		}
	}
	return false
}

func markRows(g *Grid, r Rect, gridRows uint32) {
	top, bot := r.rows(gridRows)
	for i := top; i < bot; i++ {
		g.Dirty[i] = true
	}
}

// floatRects: the floats' rects, lowest first, leaving out empty ones.
func floatRects(grids *Grids, placed []Placed) []Rect {
	out := make([]Rect, 0, len(placed))
	for _, p := range placed {
		f := grids.Get(p.Grid)
		if f == nil || f.Width == 0 || f.Height == 0 {
			continue
		}
		out = append(out, Rect{Grid: p.Grid, Row: p.Row, Col: p.Col, Width: f.Width, Height: f.Height, OnScreen: p.OnScreen, OnMsg: p.OnMsg, Top: p.Top})
	}
	return out
}

// compose: base row at terminal row r with the floats over it. Just row if
// no float covers it.
func (v *PaneView) compose(grids *Grids, row []Cell, rects []Rect, r, cols uint32) []Cell {
	width := len(row)
	covered := false
	ri := int64(r)
	for _, f := range rects {
		if ri < int64(f.Row) || ri >= int64(f.Row)+int64(f.Height) {
			continue
		}
		covered = true
		width = max(width, int(clamp64(int64(f.Col)+int64(f.Width), 0, int64(cols))))
	}
	if !covered {
		return row
	}
	width = min(width, int(cols))
	if cap(v.line) < width {
		v.line = make([]Cell, width)
	}
	line := v.line[:width]
	n0 := copy(line, row[:min(len(row), width)])
	for i := n0; i < width; i++ {
		line[i] = BlankCell(0)
	}
	for _, f := range rects {
		if ri < int64(f.Row) || ri >= int64(f.Row)+int64(f.Height) {
			continue
		}
		src := grids.Get(f.Grid).Row(uint32(ri - int64(f.Row)))
		left := int(max(f.Col, 0))
		skip := int(max(-f.Col, 0))
		if left >= width || skip >= len(src) {
			continue
		}
		n := min(len(src)-skip, width-left)
		// a wide char cut by the float's edges leaves a blank
		if left > 0 && line[left].Text == "" {
			line[left-1] = BlankCell(line[left-1].Hl)
		}
		if left+n < width && line[left+n].Text == "" {
			line[left+n] = BlankCell(line[left+n].Hl)
		}
		for k, cell := range src[skip : skip+n] {
			line[left+k] = blendCell(&grids.Style, cell, line, left+k)
		}
		if line[left].Text == "" {
			line[left] = BlankCell(line[left].Hl)
		}
	}
	return line
}

// blendCell: float cell over line[i]. A blended blank lets the text under
// it show, unless that's half a wide char.
func blendCell(style *Style, cell Cell, line []Cell, i int) Cell {
	if style.Attr(cell.Hl).Blend == 0 {
		return cell
	}
	under := line[i]
	wide := under.Text == "" || (i+1 < len(line) && line[i+1].Text == "")
	if cell.Text == " " && !wide {
		c := under
		c.Hl = style.BlendOver(cell.Hl, under.Hl, true)
		return c
	}
	c := cell
	c.Hl = style.BlendOver(cell.Hl, under.Hl, false)
	return c
}

// cursorAt: where the cursor shows in this pane, if it does.
func (v *PaneView) cursorAt(grids *Grids, g *Grid, id, gridRows uint32, msg *Grid, msgTotal, msgN uint32, rects []Rect) *Cursor {
	for _, r := range rects {
		if grids.CursorGrid != r.Grid {
			continue
		}
		f := grids.Get(r.Grid)
		row := int64(r.Row) + int64(f.CursorRow)
		col := int64(r.Col) + int64(f.CursorCol)
		if row < 0 || row >= int64(gridRows) || col < 0 || col >= int64(v.Cols) {
			return nil
		}
		return &Cursor{uint32(row), uint32(col)}
	}
	if g != nil && grids.CursorGrid == id {
		if g.CursorRow < gridRows && g.CursorCol < v.Cols {
			return &Cursor{g.CursorRow, g.CursorCol}
		}
		return nil
	}
	if msg == nil || grids.CursorGrid != grids.MsgGrid {
		return nil
	}
	first := msgTotal - msgN
	if msg.CursorRow < first || msg.CursorRow >= msgTotal || msg.CursorCol >= v.Cols {
		return nil
	}
	return &Cursor{v.Rows - msgN + (msg.CursorRow - first), msg.CursorCol}
}
