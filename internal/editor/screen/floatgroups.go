package screen

// Floats to show in a Rex floating pane. A group is an editor float with
// every float hanging from it, as a picker builds its box, input, list and
// preview; focusable groups that touch, as Telescope's do, are one. It goes
// native once something in it has taken focus: being focusable isn't
// enough, since completion menus are and are never entered. Those stay
// drawn in their pane, beside what they complete.

import "slices"

// MaxGroups is the most groups FindGroups returns.
const MaxGroups = 4

const maxTops = 16

// Group is a set of floats shown together.
type Group struct {
	tops    [maxTops]uint32
	topsLen uint8
	// Key names the group while it's open: its lowest window handle.
	Key int64
	// Where it sits on grid 1.
	Row, Col      int32
	Width, Height uint32
	Focusable     bool
	// Entered: a float in it has been Neovim's current window.
	Entered bool
}

// TopGrids are the editor floats in it, by grid.
func (g *Group) TopGrids() []uint32 { return g.tops[:g.topsLen] }

// HasTop reports whether editor float top is in the group.
func (g *Group) HasTop(top uint32) bool { return slices.Contains(g.TopGrids(), top) }

func (g *Group) bottom() int64 { return int64(g.Row) + int64(g.Height) }
func (g *Group) right() int64  { return int64(g.Col) + int64(g.Width) }

// touches: overlapping or a cell apart.
func (g *Group) touches(b *Group) bool {
	return int64(g.Row) <= b.bottom()+1 && int64(b.Row) <= g.bottom()+1 &&
		int64(g.Col) <= b.right()+1 && int64(b.Col) <= g.right()+1
}

func (g *Group) merge(b *Group) {
	g.unite(b.Row, b.Col, b.Width, b.Height)
	g.Focusable = g.Focusable || b.Focusable
	g.Entered = g.Entered || b.Entered
	g.Key = min(g.Key, b.Key)
	for _, t := range b.TopGrids() {
		if g.topsLen < maxTops {
			g.tops[g.topsLen] = t
			g.topsLen++
		}
	}
}

// unite grows the group to cover the rect too.
func (g *Group) unite(row, col int32, width, height uint32) {
	if g.Width == 0 {
		g.Row, g.Col, g.Width, g.Height = row, col, width, height
		return
	}
	top := min(g.Row, row)
	left := min(g.Col, col)
	g.Height = uint32(max(g.bottom(), int64(row)+int64(height)) - int64(top))
	g.Width = uint32(max(g.right(), int64(col)+int64(width)) - int64(left))
	g.Row, g.Col = top, left
}

// FindGroups are the groups to show natively: those with a float in
// entered, the float windows that have been current. At most MaxGroups.
func FindGroups(grids *Grids, entered []int64) []Group {
	var all []Group
	// each editor float, with the floats hanging from it
	for i, w := range grids.wins.vals {
		id := grids.wins.keys[i]
		if w.Kind != WinFloat || w.Win < 0 || len(all) == maxTops {
			continue
		}
		p, ok := grids.Place(id)
		if !ok || p.Top != id {
			continue
		}
		g := Group{Key: w.Win}
		g.tops[0], g.topsLen = id, 1
		for k, m := range grids.wins.vals {
			mid := grids.wins.keys[k]
			if m.Kind != WinFloat {
				continue
			}
			mp, ok := grids.Place(mid)
			if !ok || mp.Top != id {
				continue
			}
			grid := grids.Get(mid)
			if grid == nil || grid.Width == 0 || grid.Height == 0 {
				continue
			}
			g.unite(mp.Row, mp.Col, grid.Width, grid.Height)
			g.Focusable = g.Focusable || m.Float.Focusable
			g.Entered = g.Entered || slices.Contains(entered, m.Win)
		}
		if g.Width == 0 {
			continue
		}
		all = append(all, g)
	}
	// focusable groups that touch become one
	for merged := true; merged; {
		merged = false
	outer:
		for i := range all {
			for j := i + 1; j < len(all); j++ {
				if !all[i].Focusable || !all[j].Focusable || !all[i].touches(&all[j]) {
					continue
				}
				b := all[j]
				all[i].merge(&b)
				all[j] = all[len(all)-1]
				all = all[:len(all)-1]
				merged = true
				break outer
			}
		}
	}
	var out []Group
	for _, g := range all {
		if !g.Entered || len(out) == MaxGroups {
			continue
		}
		out = append(out, g)
	}
	return out
}

// GroupMembers are the floats in group g, lowest first.
func GroupMembers(grids *Grids, g *Group) []Placed {
	var out []Placed
	for i, w := range grids.wins.vals {
		id := grids.wins.keys[i]
		if w.Kind != WinFloat || len(out) == MaxFloats {
			continue
		}
		p, ok := grids.Place(id)
		if !ok || p.Top == 0 || !g.HasTop(p.Top) {
			continue
		}
		out = append(out, Placed{Grid: id, Row: p.Row, Col: p.Col, ZIndex: w.Float.ZIndex, CompIndex: w.Float.CompIndex, OnScreen: true, Top: p.Top})
	}
	SortPlaced(out)
	return out
}
