package rex

import (
	"strings"
	"testing"
)

type fakeReq struct {
	next    uint64
	methods []string
}

func (f *fakeReq) Request(method string, _ any) (uint64, error) {
	f.next++
	f.methods = append(f.methods, method)
	return f.next, nil
}

func blk(c string) string { return "block:" + strings.Repeat(c, 36) }

func view(rev int, focused string) any {
	return map[string]any{"revision": jsonInt(rev), "active_window_id": "w",
		"windows": []any{map[string]any{"window_id": "w", "focused_block_id": focused}}}
}

func jsonInt(n int) any { return int64(n) }

func TestSessionFlow(t *testing.T) {
	f := &fakeReq{}
	s := NewSessionWith(f, "session:s")
	s.Logf = func(string, ...any) {}
	s.Attach()                                              // id 1
	s.OnResponse(&Response{ID: 1, Result: Result{Ok: nil}}) // attached -> view id 2
	if f.methods[1] != "session.view" || s.State() != Attached {
		t.Fatal(f.methods)
	}
	// first view: layout, focus not reported (was unknown)
	u, _ := s.OnResponse(&Response{ID: 2, Result: Result{Ok: view(1, blk("a"))}})
	if _, ok := u.(Layout); !ok || !s.HasFocused(blk("a")) {
		t.Fatal(u)
	}
	// user moves focus: event, fetch, layout then FocusMoved queued
	s.OnEvent(&Event{Name: "session_view_changed", Payload: map[string]any{"revision": int64(2)}})
	u, _ = s.OnResponse(&Response{ID: 3, Result: Result{Ok: view(2, blk("b"))}})
	if _, ok := u.(Layout); !ok {
		t.Fatal(u)
	}
	if u, _ = s.Next(); u != (FocusMoved{Block: blk("b")}) {
		t.Fatal(u)
	}
	// our own focus change echoes silently
	s.Focus(blk("c")) // id 4
	s.OnResponse(&Response{ID: 4, Result: Result{Ok: map[string]any{"revision": int64(3)}}})
	s.OnEvent(&Event{Name: "session_view_changed", Payload: map[string]any{"revision": int64(3)}})
	s.OnResponse(&Response{ID: 5, Result: Result{Ok: view(3, blk("c"))}})
	if u, _ = s.Next(); u != nil {
		t.Fatal(u)
	}
	// splits and layers
	s.NewSplit(7, SplitSpec{AnchorID: blk("a"), Direction: "vertical", Side: "after"}) // 6
	u, _ = s.OnResponse(&Response{ID: 6, Result: Result{Ok: map[string]any{"block_ids": []any{blk("d")}}}})
	if u != (SplitMade{Pane: 7, Block: blk("d")}) {
		t.Fatal(u)
	}
	s.NewLayer(8, [4]float64{0, 0, 1, 1}, nil) // 7
	u, _ = s.OnResponse(&Response{ID: 7, Result: Result{Ok: map[string]any{"block_ids": []any{blk("e")}, "layer_id": "layer:x"}}})
	if u != (LayerMade{Pane: 8, Block: blk("e"), Layer: "layer:x"}) {
		t.Fatal(u)
	}
	s.NewSplit(9, SplitSpec{}) // 8
	u, _ = s.OnResponse(&Response{ID: 8, Result: Result{Err: &Error{Code: "x"}}})
	if u != (SplitFailed{Pane: 9}) {
		t.Fatal(u)
	}
	// closed: pending splits fail
	s.NewSplit(10, SplitSpec{})
	s.mu.Lock()
	s.state = Closed
	s.mu.Unlock()
	if u, _ = s.Next(); u != (SplitFailed{Pane: 10}) {
		t.Fatal(u)
	}
}
