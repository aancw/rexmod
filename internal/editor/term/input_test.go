package term

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func expectEvents(t *testing.T, feeds []string, want []Event) {
	t.Helper()
	var p Parser
	i := 0
	for _, f := range feeds {
		p.Feed([]byte(f))
		for ev, ok := p.Next(); ok; ev, ok = p.Next() {
			if i >= len(want) {
				t.Fatalf("%q: unexpected extra event: %v", feeds, ev)
			}
			w := want[i]
			if ev.Kind != w.Kind || ev.Key != w.Key || ev.Flag != w.Flag || ev.Mouse != w.Mouse || !bytes.Equal(ev.Bytes, w.Bytes) {
				t.Fatalf("%q: event %d = %v (%+v), want %v (%+v)", feeds, i, ev, ev, w, w)
			}
			i++
		}
	}
	if i != len(want) {
		t.Fatalf("%q: got %d events, want %d", feeds, i, len(want))
	}
}

func key(r rune, m Mods) Event {
	return Event{Kind: EventKey, Key: KeyEvent{Key: Key{Rune: r}, Mods: m}}
}
func named(n Named, m Mods) Event {
	return Event{Kind: EventKey, Key: KeyEvent{Key: Key{Named: n}, Mods: m}}
}
func unknown(s string) Event { return Event{Kind: EventUnknown, Bytes: []byte(s)} }
func paste(s string) Event   { return Event{Kind: EventPaste, Bytes: []byte(s)} }
func mouse(m Mouse) Event    { return Event{Kind: EventMouse, Mouse: m} }

var (
	none  = Mods{}
	ctrl  = Mods{Ctrl: true}
	alt   = Mods{Alt: true}
	shift = Mods{Shift: true}
)

func s(x ...string) []string { return x }
func e(x ...Event) []Event   { return x }

func TestFocusReportsAlone(t *testing.T) {
	for in, want := range map[string]bool{"\x1b[O": true, "\x1b[I\x1b[O": true, "\x1b[Ia": false, "\x1b[A": false} {
		if OnlyFocusReports([]byte(in)) != want {
			t.Errorf("OnlyFocusReports(%q) != %v", in, want)
		}
	}
}

func TestCharacters(t *testing.T) {
	expectEvents(t, s("a"), e(key('a', none)))
	expectEvents(t, s("aZ5"), e(key('a', none), key('Z', none), key('5', none)))
	expectEvents(t, s("é"), e(key('é', none)))
	expectEvents(t, s("漢"), e(key('漢', none)))
	expectEvents(t, s("🙂"), e(key('🙂', none)))
}

func TestControlKeys(t *testing.T) {
	expectEvents(t, s("\r"), e(named(Enter, none)))
	expectEvents(t, s("\t"), e(named(Tab, none)))
	expectEvents(t, s("\x7f"), e(named(Backspace, none)))
	expectEvents(t, s("\x01"), e(key('a', ctrl)))
	expectEvents(t, s("\x08"), e(key('h', ctrl)))
	expectEvents(t, s("\x00"), e(key(' ', ctrl)))
	expectEvents(t, s("\x1c"), e(key('\\', ctrl)))
}

func TestEscapeAndAlt(t *testing.T) {
	expectEvents(t, s("\x1b"), e(named(Escape, none)))
	expectEvents(t, s("\x1bx"), e(key('x', alt)))
	expectEvents(t, s("\x1bé"), e(key('é', alt)))
	expectEvents(t, s("\x1b\x01"), e(key('a', Mods{Ctrl: true, Alt: true})))
	expectEvents(t, s("\x1b\x1b"), e(named(Escape, none), named(Escape, none)))
	expectEvents(t, s("\x1b", "x"), e(named(Escape, none), key('x', none)))
}

func TestCSIAndSS3Keys(t *testing.T) {
	expectEvents(t, s("\x1b[A"), e(named(Up, none)))
	expectEvents(t, s("\x1b[1;5C"), e(named(Right, ctrl)))
	expectEvents(t, s("\x1b[1;4D"), e(named(Left, Mods{Shift: true, Alt: true})))
	expectEvents(t, s("\x1b[1;3A"), e(named(Up, alt)))
	expectEvents(t, s("\x1b\x1b[A"), e(named(Escape, none), named(Up, none)))
	expectEvents(t, s("\x1b[3~"), e(named(Delete, none)))
	expectEvents(t, s("\x1b[15~"), e(named(F5, none)))
	expectEvents(t, s("\x1b[24;2~"), e(named(F12, shift)))
	expectEvents(t, s("\x1bOP"), e(named(F1, none)))
	expectEvents(t, s("\x1bOA"), e(named(Up, none)))
	expectEvents(t, s("\x1b[Z"), e(named(Tab, shift)))
	expectEvents(t, s("\x1b[97;5u"), e(key('a', ctrl)))
	expectEvents(t, s("\x1b[13;2u"), e(named(Enter, shift)))
}

func TestKittyKeyboardProtocol(t *testing.T) {
	expectEvents(t, s("\x1b[27u"), e(named(Escape, none)))
	expectEvents(t, s("\x1b[105;5u"), e(key('i', ctrl)))
	expectEvents(t, s("\x1b[104;5u"), e(key('h', ctrl)))
	expectEvents(t, s("\x1b[120;3u"), e(key('x', alt)))
	expectEvents(t, s("\x1b[13;5u"), e(named(Enter, ctrl)))
	expectEvents(t, s("\x1b[49:33;4u"), e(key('!', alt)))
	expectEvents(t, s("\x1b[97:65;6u"), e(key('a', Mods{Ctrl: true, Shift: true})))
	expectEvents(t, s("\x1b[97;69u"), e(key('a', ctrl)))
	expectEvents(t, s("\x1b[97;5:3u"), nil)
	expectEvents(t, s("\x1b[57399u\x1b[57414u"), e(key('0', none), named(Enter, none)))
	expectEvents(t, s("\x1b[57441;2u"), e(unknown("\x1b[57441;2u")))
	expectEvents(t, s("\x1b[1;5:1C"), e(named(Right, ctrl)))
}

func TestSplitAcrossReads(t *testing.T) {
	expectEvents(t, s("\x1b[1;", "5C"), e(named(Right, ctrl)))
	expectEvents(t, s("\x1b[", "A"), e(named(Up, none)))
	expectEvents(t, s("\xc3", "\xa9"), e(key('é', none)))
}

func TestThemeReports(t *testing.T) {
	expectEvents(t, s("\x1b[?997;1n", "\x1b[?997;2n"), e(Event{Kind: EventDarkTheme, Flag: true}, Event{Kind: EventDarkTheme, Flag: false}))
	expectEvents(t, s("\x1b[?997;3n"), e(response("\x1b[?997;3n")))
}

func TestUnknownAndBroken(t *testing.T) {
	expectEvents(t, s("\x1b[99X"), e(unknown("\x1b[99X")))
	expectEvents(t, s("\x1b]11;rgb:0/0/0\x07a"), e(response("\x1b]11;rgb:0/0/0\x07"), key('a', none)))
	expectEvents(t, s("\xc3a"), e(key(utf8.RuneError, none), key('a', none)))
	expectEvents(t, s("\x80"), e(key(utf8.RuneError, none)))
}

func TestPasteFocusMouse(t *testing.T) {
	ps, pe := Event{Kind: EventPasteStart}, Event{Kind: EventPasteEnd}
	expectEvents(t, s("\x1b[200~hi\r\x1b[201~"), e(ps, paste("hi\r"), pe))
	expectEvents(t, s("\x1b[200~a", "b\x1b[201~"), e(ps, paste("a"), paste("b"), pe))
	expectEvents(t, s("\x1b[200~\x1b[A\x1b[201~"), e(ps, paste("\x1b[A"), pe))
	expectEvents(t, s("\x1b[I\x1b[O"), e(Event{Kind: EventFocus, Flag: true}, Event{Kind: EventFocus}))
	expectEvents(t, s("\x1b[<0;10;5M"), e(mouse(Mouse{Button: MouseLeft, Action: Press, Row: 4, Col: 9})))
	expectEvents(t, s("\x1b[<0;10;5m"), e(mouse(Mouse{Button: MouseLeft, Action: Release, Row: 4, Col: 9})))
	expectEvents(t, s("\x1b[<64;1;1M"), e(mouse(Mouse{Button: WheelUp, Action: Press})))
	expectEvents(t, s("\x1b[<48;3;2M"), e(mouse(Mouse{Button: MouseLeft, Action: Drag, Mods: ctrl, Row: 1, Col: 2})))
}

func TestNeovimNotation(t *testing.T) {
	cases := []struct {
		ev   KeyEvent
		want string
	}{
		{KeyEvent{Key: Key{Rune: 'a'}}, "a"},
		{KeyEvent{Key: Key{Rune: '<'}}, "<lt>"},
		{KeyEvent{Key: Key{Rune: ' '}}, " "},
		{KeyEvent{Key: Key{Rune: ' '}, Mods: ctrl}, "<C-Space>"},
		{KeyEvent{Key: Key{Rune: 'x'}, Mods: alt}, "<M-x>"},
		{KeyEvent{Key: Key{Rune: 'é'}}, "é"},
		{KeyEvent{Key: Key{Named: Enter}}, "<CR>"},
		{KeyEvent{Key: Key{Named: Right}, Mods: ctrl}, "<C-Right>"},
		{KeyEvent{Key: Key{Named: F5}, Mods: shift}, "<S-F5>"},
		{KeyEvent{Key: Key{Rune: 'i'}, Mods: ctrl}, "<C-i>"},
		{KeyEvent{Key: Key{Rune: 'a'}, Mods: Mods{Ctrl: true, Shift: true}}, "<C-S-a>"},
		{KeyEvent{Key: Key{Rune: '!'}, Mods: alt}, "<M-!>"},
	}
	for _, c := range cases {
		if got := c.ev.Nvim(); got != c.want {
			t.Errorf("%+v: got %q, want %q", c.ev, got, c.want)
		}
	}
}

// Replies to queries an editor plugin sent the terminal are never keys.
func TestTermResponses(t *testing.T) {
	for _, s := range []string{
		"\x1bP>|rex 0.0.1793390926-g3b2dce\x1b\\", // XTVERSION
		"\x1b[?62;22c",                   // DA1
		"\x1b]11;rgb:0000/0000/0000\x07", // OSC 11
		"\x1b_Gi=31;OK\x1b\\",            // kitty graphics
		"\x1b[?2026;2$y",                 // DECRPM
	} {
		var p Parser
		p.Feed([]byte(s))
		e, ok := p.Next()
		if !ok || e.Kind != EventTermResponse || string(e.Bytes) != s {
			t.Errorf("%q: got %v", s, e)
		}
		if e, ok := p.Next(); ok {
			t.Errorf("%q: extra event %v", s, e)
		}
	}
	// split across reads
	var p Parser
	p.Feed([]byte("\x1bP>|re"))
	if e, ok := p.Next(); ok {
		t.Fatalf("early event %v", e)
	}
	p.Feed([]byte("x\x1b\\j"))
	e, _ := p.Next()
	if e.Kind != EventTermResponse {
		t.Fatalf("got %v", e)
	}
	if e, _ := p.Next(); e.Kind != EventKey || e.Key.Nvim() != "j" {
		t.Fatalf("then %v", e)
	}
}

func response(b string) Event { return Event{Kind: EventTermResponse, Bytes: []byte(b)} }
