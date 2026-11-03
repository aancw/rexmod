package screen

import (
	"math"
	"slices"
	"sort"
	"strings"
)

// WinKind is where a window lives.
type WinKind uint8

const (
	// WinOrdinary is in the tab's split layout, positioned on grid 1.
	WinOrdinary WinKind = iota
	// WinExternal is outside the layout, for the UI to place.
	WinExternal
	// WinFloat floats over another grid, such as the popup menu.
	WinFloat
	// WinHidden is an ordinary window on another tab page.
	WinHidden
)

// Win is a window on a grid.
type Win struct {
	// Win is Neovim's window handle; -1 for the popup menu.
	Win  int64
	Kind WinKind
	// Float is where it floats, for WinFloat.
	Float Float
}

// Anchor is the corner of a float placed at its position.
type Anchor uint8

const (
	AnchorNW Anchor = iota
	AnchorNE
	AnchorSW
	AnchorSE
)

// Float is a float's position, as win_float_pos gives it. Start from
// [DefaultFloat].
type Float struct {
	// AnchorGrid is the grid it floats over; Anchor the corner at Row, Col.
	AnchorGrid uint32
	Anchor     Anchor
	Row, Col   float64
	ZIndex     uint32
	// Focusable: takes the mouse; Neovim sets this for focusable floats.
	Focusable bool
	// CompIndex is Neovim's drawing order among equal zindexes.
	CompIndex uint32
	// Screen is where Neovim put it on grid 1, kept inside the screen.
	Screen    [2]int32
	HasScreen bool
}

// DefaultFloat is a float's position before win_float_pos fills it in.
func DefaultFloat() Float {
	return Float{AnchorGrid: 1, ZIndex: 50, Focusable: true}
}

// Placed is a float placed over a base grid, in that grid's cells.
type Placed struct {
	Grid      uint32
	Row, Col  int32
	ZIndex    uint32
	CompIndex uint32
	// OnScreen: on the screen (grid 1 or the message grid), not over a window.
	OnScreen bool
	// OnMsg: on the message grid, which shows at the bottom.
	OnMsg bool
	// Top is the editor-relative float it hangs from, itself included; 0
	// if none.
	Top uint32
}

// Place is where a float sits on the grid under it all.
type Place struct {
	Root     uint32
	Row, Col int32
	Top      uint32
}

// MaxFloats is the most floats drawn over one pane.
const MaxFloats = 16

// Grids is every grid Neovim draws, built from its redraw events. With
// ext_multigrid each window has its own grid; grid 1 holds what's left
// (statuslines of split windows, the tabline). Also tracks where windows
// and the message grid are.
type Grids struct {
	grids ordered[*Grid]
	// windows by grid id
	wins  ordered[Win]
	Style Style
	// CursorGrid is the grid the cursor is on.
	CursorGrid uint32
	// MsgGrid is the message grid, drawn over grid 1 from MsgRow down; 0
	// for none yet.
	MsgGrid uint32
	MsgRow  uint32
}

// NewGrids is an empty set of grids, the cursor on grid 1.
func NewGrids() *Grids { return &Grids{CursorGrid: 1} }

// Get is grid id, nil if it doesn't exist.
func (gs *Grids) Get(id uint32) *Grid {
	if p := gs.grids.ptr(id); p != nil {
		return *p
	}
	return nil
}

// GridIDs are the grids' ids, in Neovim's order of creation (removal
// swaps the last into the gap).
func (gs *Grids) GridIDs() []uint32 { return slices.Clone(gs.grids.keys) }

// WinGrids are the grids that have windows, in the same kind of order.
func (gs *Grids) WinGrids() []uint32 { return slices.Clone(gs.wins.keys) }

// GridOf is the grid showing window win.
func (gs *Grids) GridOf(win int64) (uint32, bool) {
	for i, w := range gs.wins.vals {
		if w.Win == win {
			return gs.wins.keys[i], true
		}
	}
	return 0, false
}

// WinOn is the window on grid.
func (gs *Grids) WinOn(grid uint32) (Win, bool) {
	if p := gs.wins.ptr(grid); p != nil {
		return *p, true
	}
	return Win{}, false
}

// FloatsOver are the floats over grid base, and over the screen too if
// screen (floats on grid 1 and the message grid), lowest first. Floats
// hanging from an editor float in skip are left out. At most MaxFloats.
func (gs *Grids) FloatsOver(base uint32, screen bool, skip []uint32) []Placed {
	var out []Placed
	for i, w := range gs.wins.vals {
		id := gs.wins.keys[i]
		if w.Kind != WinFloat || len(out) == MaxFloats {
			continue
		}
		p, ok := gs.Place(id)
		if !ok {
			continue
		}
		if p.Top != 0 && slices.Contains(skip, p.Top) {
			continue
		}
		onMsg := gs.MsgGrid != 0 && p.Root == gs.MsgGrid
		onScreen := p.Root == 1 || onMsg
		if p.Root != base && !(screen && onScreen) {
			continue
		}
		out = append(out, Placed{Grid: id, Row: p.Row, Col: p.Col, ZIndex: w.Float.ZIndex, CompIndex: w.Float.CompIndex, OnScreen: onScreen, OnMsg: onMsg, Top: p.Top})
	}
	SortPlaced(out)
	return out
}

// SortPlaced sorts lowest first, as Neovim stacks them.
func SortPlaced(placed []Placed) {
	sort.SliceStable(placed, func(i, j int) bool {
		a, b := placed[i], placed[j]
		if a.ZIndex != b.ZIndex {
			return a.ZIndex < b.ZIndex
		}
		return a.CompIndex < b.CompIndex
	})
}

// Place is where float grid id sits on the grid under it all: floats can
// float over floats.
func (gs *Grids) Place(id uint32) (Place, bool) {
	w, ok := gs.WinOn(id)
	if !ok || w.Kind != WinFloat {
		return Place{}, false
	}
	var row, col int32
	grid := id
	float := w.Float
	var top uint32
	for range 8 {
		g := gs.Get(grid)
		if g == nil {
			return Place{}, false
		}
		if float.AnchorGrid == 1 {
			top = grid
			if float.HasScreen {
				return Place{Root: 1, Row: row + float.Screen[0], Col: col + float.Screen[1], Top: top}, true
			}
		}
		row += int32(math.Floor(float.Row))
		if float.Anchor == AnchorSW || float.Anchor == AnchorSE {
			row -= int32(g.Height)
		}
		col += int32(math.Floor(float.Col))
		if float.Anchor == AnchorNE || float.Anchor == AnchorSE {
			col -= int32(g.Width)
		}
		grid = float.AnchorGrid
		under, ok := gs.WinOn(grid)
		if !ok {
			// the message grid sits on grid 1 at MsgRow
			if gs.MsgGrid != 0 && grid == gs.MsgGrid {
				return Place{Root: grid, Row: row + int32(gs.MsgRow), Col: col, Top: top}, true
			}
			return Place{Root: grid, Row: row, Col: col, Top: top}, true
		}
		if under.Kind != WinFloat {
			return Place{Root: grid, Row: row, Col: col, Top: top}, true
		}
		float = under.Float
	}
	return Place{}, false
}

// MsgRows are the rows the message grid covers at the bottom of grid 1:
// the command line, or more while a long message scrolls up.
func (gs *Grids) MsgRows() uint32 {
	if gs.MsgGrid == 0 {
		return 0
	}
	screen, msg := gs.Get(1), gs.Get(gs.MsgGrid)
	if screen == nil || msg == nil {
		return 0
	}
	return min(satSub(screen.Height, gs.MsgRow), msg.Height)
}

// ApplyRedraw applies a "redraw" notification's params,
// [[name, args, args, ...], ...]. flushed is true if it ended a frame with
// flush, so it's time to draw. Unknown events are skipped.
func (gs *Grids) ApplyRedraw(params []any) (flushed bool, err error) {
	for _, ev := range params {
		batch, err := expectArray(ev)
		if err != nil {
			return flushed, err
		}
		if len(batch) == 0 {
			return flushed, ErrBadMessage
		}
		name, err := expectStr(batch[0])
		if err != nil {
			return flushed, err
		}
		if !knownEvents[name] {
			continue
		}
		for _, a := range batch[1:] {
			args, err := expectArray(a)
			if err != nil {
				return flushed, err
			}
			if err := gs.apply(name, args); err != nil {
				return flushed, err
			}
			if name == "flush" {
				flushed = true
			}
		}
	}
	return flushed, nil
}

var knownEvents = map[string]bool{
	"grid_resize": true, "grid_clear": true, "grid_line": true, "grid_scroll": true,
	"grid_cursor_goto": true, "grid_destroy": true, "win_pos": true, "win_external_pos": true,
	"win_float_pos": true, "win_hide": true, "win_close": true, "msg_set_pos": true,
	"hl_attr_define": true, "default_colors_set": true, "mode_info_set": true,
	"mode_change": true, "busy_start": true, "busy_stop": true, "flush": true,
}

// u32s reads the first n args as uint32s.
func u32s(args []any, n int) ([]uint32, error) {
	if len(args) < n {
		return nil, ErrBadMessage
	}
	out := make([]uint32, n)
	for i := range out {
		v, err := expectU32(args[i])
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (gs *Grids) apply(event string, args []any) error {
	switch event {
	case "grid_resize":
		v, err := u32s(args, 3)
		if err != nil {
			return err
		}
		g := gs.Get(v[0])
		if g == nil {
			g = &Grid{}
			gs.grids.put(v[0], g)
		}
		g.Resize(v[1], v[2])
	case "grid_clear":
		v, err := u32s(args, 1)
		if err != nil {
			return err
		}
		if g := gs.Get(v[0]); g != nil {
			g.Clear()
		}
	case "grid_line":
		if len(args) < 4 {
			return ErrBadMessage
		}
		v, err := u32s(args, 3)
		if err != nil {
			return err
		}
		if g := gs.Get(v[0]); g != nil {
			return g.line(args[3], v[1], v[2])
		}
	case "grid_scroll":
		if len(args) < 7 {
			return ErrBadMessage
		}
		v, err := u32s(args, 5)
		if err != nil {
			return err
		}
		rows, err := expectInt(args[5])
		if err != nil {
			return err
		}
		if _, err := expectInt(args[6]); err != nil { // cols, always 0
			return err
		}
		if g := gs.Get(v[0]); g != nil {
			return g.Scroll(v[1], v[2], v[3], v[4], rows)
		}
	case "grid_cursor_goto":
		v, err := u32s(args, 3)
		if err != nil {
			return err
		}
		gs.CursorGrid = v[0]
		if g := gs.Get(v[0]); g != nil {
			g.CursorRow, g.CursorCol = v[1], v[2]
		}
	case "grid_destroy":
		v, err := u32s(args, 1)
		if err != nil {
			return err
		}
		gs.grids.swapRemove(v[0])
		gs.wins.swapRemove(v[0])
	case "win_pos", "win_external_pos", "win_float_pos":
		if len(args) < 2 {
			return ErrBadMessage
		}
		id, err := expectU32(args[0])
		if err != nil {
			return err
		}
		win, err := expectHandle(args[1])
		if err != nil {
			return err
		}
		switch event {
		case "win_pos":
			gs.wins.put(id, Win{Win: win, Kind: WinOrdinary})
			return nil
		case "win_external_pos":
			gs.wins.put(id, Win{Win: win, Kind: WinExternal})
			return nil
		}
		if len(args) < 8 {
			return ErrBadMessage
		}
		f := DefaultFloat()
		anchor, err := expectStr(args[2])
		if err != nil {
			return err
		}
		switch strings.ToLower(anchor) {
		case "ne":
			f.Anchor = AnchorNE
		case "sw":
			f.Anchor = AnchorSW
		case "se":
			f.Anchor = AnchorSE
		default:
			f.Anchor = AnchorNW
		}
		if f.AnchorGrid, err = expectU32(args[3]); err != nil {
			return err
		}
		if f.Row, err = expectNumber(args[4]); err != nil {
			return err
		}
		if f.Col, err = expectNumber(args[5]); err != nil {
			return err
		}
		if f.Focusable, err = expectBool(args[6]); err != nil {
			return err
		}
		if f.ZIndex, err = expectU32(args[7]); err != nil {
			return err
		}
		if len(args) >= 11 {
			if f.CompIndex, err = expectU32(args[8]); err != nil {
				return err
			}
			if f.Screen[0], err = expectI32(args[9]); err != nil {
				return err
			}
			if f.Screen[1], err = expectI32(args[10]); err != nil {
				return err
			}
			f.HasScreen = true
		}
		gs.wins.put(id, Win{Win: win, Kind: WinFloat, Float: f})
	case "win_hide":
		v, err := u32s(args, 1)
		if err != nil {
			return err
		}
		if w := gs.wins.ptr(v[0]); w != nil {
			w.Kind = WinHidden
		}
	case "win_close":
		v, err := u32s(args, 1)
		if err != nil {
			return err
		}
		gs.wins.swapRemove(v[0])
	case "msg_set_pos":
		v, err := u32s(args, 2)
		if err != nil {
			return err
		}
		gs.MsgGrid, gs.MsgRow = v[0], v[1]
	case "flush":
	default:
		return gs.Style.apply(event, args)
	}
	return nil
}
