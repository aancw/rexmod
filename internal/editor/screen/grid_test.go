package screen

import (
	"slices"
	"testing"
)

func rowsText(g *Grid) []string {
	var out []string
	for r := range g.Height {
		out = append(out, g.RowText(r))
	}
	return out
}

func TestResizeKeepsWhatFits(t *testing.T) {
	g := resized(3, 2)
	for k := range g.Cells {
		g.Cells[k] = NewCell(string(rune('a'+k)), 0)
	}
	g.ClearDirty()
	g.Resize(2, 3)
	if got := rowsText(g); !slices.Equal(got, []string{"ab", "de", "  "}) {
		t.Fatal(got)
	}
	eq(t, g.DirtyCount(), 3)
}

func TestScrollUpAndDown(t *testing.T) {
	g := resized(2, 5)
	for r := range 5 {
		g.Cells[r*2] = NewCell(string(rune('a'+r)), 0)
	}
	// rows 1..4 up by 1: rows 1..3 get rows 2..4; row 3 is left for grid_line
	if err := g.Scroll(1, 4, 0, 2, 1); err != nil {
		t.Fatal(err)
	}
	if got := rowsText(g); !slices.Equal(got, []string{"a ", "c ", "d ", "d ", "e "}) {
		t.Fatal(got)
	}
	// rows 0..5 down by 2: rows 2..5 get rows 0..3; rows 0..2 are left
	if err := g.Scroll(0, 5, 0, 2, -2); err != nil {
		t.Fatal(err)
	}
	if got := rowsText(g); !slices.Equal(got, []string{"a ", "c ", "a ", "c ", "d "}) {
		t.Fatal(got)
	}
}

func TestFullWidthScrollsKept(t *testing.T) {
	g := resized(2, 5)
	g.ClearDirty()
	g.Dirty[3] = true

	if err := g.Scroll(0, 5, 0, 2, 2); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(g.PendingScrolls(), []Scroll{{Top: 0, Bot: 5, Rows: 2}}) {
		t.Fatal(g.PendingScrolls())
	}
	// row 3 moved to 1; rows 3 and 4 were left behind
	if !slices.Equal(g.Dirty, []bool{false, true, false, true, true}) {
		t.Fatal(g.Dirty)
	}

	// not full width: no scroll kept, the region is redrawn
	g.ClearDirty()
	if err := g.Scroll(1, 3, 0, 1, -1); err != nil {
		t.Fatal(err)
	}
	eq(t, len(g.PendingScrolls()), 1)
	if !slices.Equal(g.Dirty, []bool{false, true, true, false, false}) {
		t.Fatal(g.Dirty)
	}
}
