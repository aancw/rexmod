package screen

import "testing"

// tmap is a Map in wire order.
type tmap [][2]any

func (m tmap) Len() int                { return len(m) }
func (m tmap) At(i int) (key, val any) { return m[i][0], m[i][1] }

// handle is a window handle as Neovim's ext 1.
type handle int64

func (h handle) HandleValue() (int64, bool) { return int64(h), true }

func i(n int64) any { return n }

// ev is [name, [args...]].
func ev(name string, args ...any) any { return []any{name, args} }

func ints(ns ...int64) []any {
	out := make([]any, len(ns))
	for k, n := range ns {
		out[k] = n
	}
	return out
}

func iev(name string, ns ...int64) any { return ev(name, ints(ns...)...) }

func winEvent(name string, grid int64, win int64) any { return ev(name, grid, handle(win)) }

// lineEvent: ["grid_line", [grid, row, col, [[text, 0]], false]]
func lineEvent(grid, r, col int64, text string) any {
	return ev("grid_line", grid, r, col, []any{[]any{text, int64(0)}}, false)
}

func floatEvent(grid int64, anchor string, anchorGrid int64, row, col float64, zindex int64, screen *[2]int64) any {
	args := []any{grid, handle(1008), anchor, anchorGrid, row, col, true, zindex}
	if screen != nil {
		args = append(args, int64(0), screen[0], screen[1])
	}
	return ev("win_float_pos", args...)
}

func apply(t *testing.T, gs *Grids, events ...any) bool {
	t.Helper()
	flushed, err := gs.ApplyRedraw(events)
	if err != nil {
		t.Fatal(err)
	}
	return flushed
}

func eq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func resized(w, h uint32) *Grid {
	g := &Grid{}
	g.Resize(w, h)
	return g
}
