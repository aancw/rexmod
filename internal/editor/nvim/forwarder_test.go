package nvim

import (
	"testing"

	"github.com/alpcanaydin/rexmod/internal/editor/term"
)

func TestMouseArgs(t *testing.T) {
	drag, ok := mouseArgsFrom(term.Mouse{Button: term.MouseLeft, Action: term.Drag, Mods: term.Mods{Ctrl: true, Shift: true}, Row: 3, Col: 7})
	if !ok || drag.button != "left" || drag.action != "drag" || drag.mods != "C-S-" {
		t.Fatalf("drag: %+v", drag)
	}
	wheel, ok := mouseArgsFrom(term.Mouse{Button: term.WheelDown, Action: term.Press})
	if !ok || wheel.button != "wheel" || wheel.action != "down" || wheel.mods != "" {
		t.Fatalf("wheel: %+v", wheel)
	}
	move, ok := mouseArgsFrom(term.Mouse{Button: term.MouseNone, Action: term.Motion})
	if !ok || move.button != "move" {
		t.Fatalf("move: %+v", move)
	}
	if _, ok := mouseArgsFrom(term.Mouse{Button: term.MouseNone, Action: term.Release}); ok {
		t.Fatal("release without a button has no name")
	}
}

func TestRouteToFloat(t *testing.T) {
	f := NewForwarder()
	f.Grid = 2
	f.SetTargets([]Target{{Grid: 3, Row: 1, Col: 1, Width: 10, Height: 5}, {Grid: 4, Row: 2, Col: 2, Width: 3, Height: 1}})
	if r := f.route(term.Mouse{Button: term.MouseLeft, Action: term.Press}); r.grid != 2 || r.row != 0 {
		t.Fatalf("off: %+v", r)
	}
	f.route(term.Mouse{Button: term.MouseLeft, Action: term.Release})
	if r := f.route(term.Mouse{Button: term.MouseLeft, Action: term.Press, Row: 2, Col: 3}); r.grid != 4 || r.row != 0 || r.col != 1 {
		t.Fatalf("top: %+v", r)
	}
	if r := f.route(term.Mouse{Button: term.MouseLeft, Action: term.Drag, Row: 0, Col: 9}); r.grid != 4 || r.row != 0 || r.col != 7 {
		t.Fatalf("drag: %+v", r)
	}
	f.route(term.Mouse{Button: term.MouseLeft, Action: term.Release, Col: 9})
	if f.grab != nil {
		t.Fatal("grab kept")
	}
	if r := f.route(term.Mouse{Button: term.WheelDown, Action: term.Press, Row: 4, Col: 1}); r.grid != 3 || r.row != 3 {
		t.Fatalf("under: %+v", r)
	}
}
