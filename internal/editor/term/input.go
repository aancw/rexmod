// Package term handles the terminal an editor runs in: parsing the bytes it
// sends into events, the modes to turn on and off, raw mode, and signals.
// Ported from neorex (MIT).
//
// Keys arrive in a few shapes:
//
//	a, é, 漢         the character itself (UTF-8, 1 to 4 bytes)
//	Enter, Tab       single control bytes: 0x0d, 0x09
//	Ctrl+A .. Ctrl+Z control bytes 0x01 .. 0x1a
//	Escape           0x1b on its own
//	Alt+x            0x1b then x
//	Up, Ctrl+Right   CSI sequences: ESC [ 1 ; 5 C
//	Delete, F5       CSI ... ~ : ESC [ 3 ~, ESC [ 1 5 ~
//	F1 (some terms)  SS3 sequences: ESC O P
//
// With the kitty keyboard protocol on (KittyPush), keys the legacy encoding
// garbles arrive as "CSI u": ESC [ code : shifted ; mods : event u.
// Spec: https://sw.kovidgoyal.net/kitty/keyboard-protocol/
//
// The Parser is a byte-at-a-time state machine whose state survives between
// reads, so a sequence split across reads carries on.
package term

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// KittyPush turns the kitty keyboard protocol on with flags 1 (disambiguate)
// and 4 (report the shifted key). Push after switching screens.
const KittyPush = "\x1b[>5u"

// KittyPop pops what KittyPush pushed.
const KittyPop = "\x1b[<u"

// Mode 2031: the terminal reports dark/light theme changes as DarkTheme events.
// https://contour-terminal.org/vt-extensions/color-palette-update-notifications/
const (
	ThemeReportsOn  = "\x1b[?2031h"
	ThemeReportsOff = "\x1b[?2031l"
	// ThemeQuery asks for the theme now; the answer is a DarkTheme event.
	ThemeQuery = "\x1b[?996n"
)

// Mods are modifier keys held with a key or mouse event.
type Mods struct {
	Shift, Alt, Ctrl, Super bool
}

// ModsFromParam decodes the modifier parameter in sequences like ESC [ 1 ; 5 C:
// value minus one is bits 1 shift, 2 alt, 4 ctrl, 8 super. Higher bits are ignored.
func ModsFromParam(param uint32) Mods {
	if param <= 1 {
		return Mods{}
	}
	b := param - 1
	return Mods{Shift: b&1 != 0, Alt: b&2 != 0, Ctrl: b&4 != 0, Super: b&8 != 0}
}

// IsEmpty reports whether no modifier is held.
func (m Mods) IsEmpty() bool { return !m.Shift && !m.Alt && !m.Ctrl && !m.Super }

// Named is a key that isn't a character.
type Named uint8

const (
	NamedNone Named = iota
	Enter
	Tab
	Backspace
	Escape
	Up
	Down
	Left
	Right
	Home
	End
	Insert
	Delete
	PageUp
	PageDown
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
)

var nvimNames = [...]string{
	Enter: "CR", Tab: "Tab", Backspace: "BS", Escape: "Esc", Up: "Up", Down: "Down",
	Left: "Left", Right: "Right", Home: "Home", End: "End", Insert: "Insert", Delete: "Del",
	PageUp: "PageUp", PageDown: "PageDown", F1: "F1", F2: "F2", F3: "F3", F4: "F4",
	F5: "F5", F6: "F6", F7: "F7", F8: "F8", F9: "F9", F10: "F10", F11: "F11", F12: "F12",
}

// NvimName is the key's name in Neovim notation, without brackets.
func (n Named) NvimName() string {
	if int(n) < len(nvimNames) {
		return nvimNames[n]
	}
	return ""
}

// Key is a character or a named key. When Named is NamedNone, Rune is the
// character; Ctrl+A is 'a' with Ctrl set.
type Key struct {
	Rune  rune
	Named Named
}

// KeyEvent is a key with its modifiers.
type KeyEvent struct {
	Key  Key
	Mods Mods
}

// Nvim returns the key in Neovim's key notation, as nvim_input takes it:
// a, <lt>, <CR>, <C-Right>, <M-x>, <C-Space>.
func (e KeyEvent) Nvim() string {
	var name string
	if e.Key.Named != NamedNone {
		name = e.Key.Named.NvimName()
	} else {
		switch e.Key.Rune {
		case '<':
			name = "lt"
		case ' ':
			if !e.Mods.IsEmpty() {
				name = "Space"
			}
		}
	}
	bracket := name != "" || !e.Mods.IsEmpty()
	var b strings.Builder
	if bracket {
		b.WriteByte('<')
	}
	if e.Mods.Ctrl {
		b.WriteString("C-")
	}
	if e.Mods.Alt {
		b.WriteString("M-")
	}
	if e.Mods.Shift {
		b.WriteString("S-")
	}
	if e.Mods.Super {
		b.WriteString("D-")
	}
	if name != "" {
		b.WriteString(name)
	} else {
		// invalid runes (lone surrogates) become U+FFFD
		b.WriteRune(e.Key.Rune)
	}
	if bracket {
		b.WriteByte('>')
	}
	return b.String()
}

// MouseButton is the button in a mouse event.
type MouseButton uint8

const (
	MouseLeft MouseButton = iota
	MouseMiddle
	MouseRight
	MouseNone
	WheelUp
	WheelDown
	WheelLeft
	WheelRight
)

var buttonNames = [...]string{"left", "middle", "right", "none", "wheel_up", "wheel_down", "wheel_left", "wheel_right"}

func (b MouseButton) String() string { return buttonNames[b] }

// MouseAction is what the mouse did.
type MouseAction uint8

const (
	Press MouseAction = iota
	Release
	Drag
	Motion
)

var actionNames = [...]string{"press", "release", "drag", "motion"}

func (a MouseAction) String() string { return actionNames[a] }

// Mouse is an SGR mouse report. Row and Col count from 0.
type Mouse struct {
	Button   MouseButton
	Action   MouseAction
	Mods     Mods
	Row, Col uint16
}

// EventKind says which field of Event is set.
type EventKind uint8

const (
	EventKey EventKind = iota
	// EventPasteStart, then text in EventPaste chunks, then EventPasteEnd.
	// Pasted text never turns into key events.
	EventPasteStart
	EventPaste
	EventPasteEnd
	EventFocus
	EventDarkTheme
	EventMouse
	// EventTermResponse is the terminal answering a query (DCS, OSC, APC,
	// or a CSI reply such as DA1), in Bytes. It goes to Neovim as a
	// termresponse, never as keys.
	EventTermResponse
	EventUnknown
)

// Event is one thing the terminal sent.
type Event struct {
	Kind EventKind
	Key  KeyEvent
	// Bytes is the pasted text (EventPaste) or raw sequence (EventUnknown).
	// It points into the fed bytes or parser memory: valid until the next
	// Next or Feed.
	Bytes []byte
	// Flag is focus gained (EventFocus) or dark theme (EventDarkTheme).
	Flag  bool
	Mouse Mouse
}

func (e Event) String() string {
	switch e.Kind {
	case EventKey:
		return "key " + e.Key.Nvim()
	case EventPasteStart:
		return "paste start"
	case EventPaste:
		return fmt.Sprintf("paste \"%s\"", hexEscape(e.Bytes))
	case EventPasteEnd:
		return "paste end"
	case EventFocus:
		if e.Flag {
			return "focus in"
		}
		return "focus out"
	case EventDarkTheme:
		if e.Flag {
			return "theme dark"
		}
		return "theme light"
	case EventMouse:
		m := e.Mouse
		s := fmt.Sprintf("mouse %s %s row=%d col=%d", m.Button, m.Action, m.Row, m.Col)
		if !m.Mods.IsEmpty() {
			var parts []string
			for _, p := range []struct {
				on   bool
				name string
			}{{m.Mods.Ctrl, "ctrl"}, {m.Mods.Alt, "alt"}, {m.Mods.Shift, "shift"}, {m.Mods.Super, "super"}} {
				if p.on {
					parts = append(parts, p.name)
				}
			}
			s += " (" + strings.Join(parts, "+") + ")"
		}
		return s
	case EventTermResponse:
		return fmt.Sprintf("termresponse \"%s\"", hexEscape(e.Bytes))
	case EventUnknown:
		return fmt.Sprintf("unknown \"%s\"", hexEscape(e.Bytes))
	}
	return "?"
}

func hexEscape(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		if c >= 0x20 && c <= 0x7e && c != '\\' && c != '"' {
			s.WriteByte(c)
		} else {
			fmt.Fprintf(&s, "\\x%02x", c)
		}
	}
	return s.String()
}

// OnlyFocusReports reports whether b holds nothing but focus reports (CSI I, CSI O).
func OnlyFocusReports(b []byte) bool {
	for len(b) > 0 {
		if !bytes.HasPrefix(b, []byte("\x1b[I")) && !bytes.HasPrefix(b, []byte("\x1b[O")) {
			return false
		}
		b = b[3:]
	}
	return true
}

const (
	maxParams   = 16
	maxSequence = 64
)

type state uint8

const (
	stGround state = iota
	stUTF8
	stEscape
	stCSI
	stSS3
	stOSC
	stOSCEscape
)

var escByte = []byte{0x1b}

// Parser turns terminal input bytes into events. The zero value is ready.
//
//	p.Feed(buf[:n])
//	for ev, ok := p.Next(); ok; ev, ok = p.Next() { ... }
type Parser struct {
	state state
	input []byte
	pos   int

	sequence    [maxSequence]byte
	sequenceLen int

	// CSI params; sub-params (after ':') are in the same list, marked in isSub.
	params       [maxParams]uint32
	isSub        uint16
	paramCount   int
	current      uint32
	hasCurrent   bool
	currentIsSub bool
	marker       byte
	codepoint    rune
	utf8Left     int
	alt          bool
	inPaste      bool
}

// Feed gives the parser the bytes from one read. Call Next until it returns
// false before feeding more.
func (p *Parser) Feed(b []byte) {
	if p.pos < len(p.input) {
		panic("term: Feed before input was used up")
	}
	p.input = b
	p.pos = 0
}

// Next returns the next event from the fed bytes, or false when they are used up.
func (p *Parser) Next() (Event, bool) {
	for p.pos < len(p.input) {
		// inside a paste, plain bytes pass through in runs
		if p.inPaste && p.state == stGround {
			rest := p.input[p.pos:]
			run := bytes.IndexByte(rest, 0x1b)
			if run < 0 {
				run = len(rest)
			}
			if run > 0 {
				p.pos += run
				return Event{Kind: EventPaste, Bytes: rest[:run]}, true
			}
		}
		b := p.input[p.pos]
		p.pos++
		if ev, ok := p.step(b); ok {
			return ev, true
		}
	}
	// the read ended right after an ESC: it was the Escape key itself
	if p.state == stEscape {
		p.state = stGround
		if p.inPaste {
			return Event{Kind: EventPaste, Bytes: escByte}, true
		}
		return p.keyEvent(Key{Named: Escape}, Mods{}), true
	}
	return Event{}, false
}

func (p *Parser) seq() []byte { return p.sequence[:p.sequenceLen] }

func (p *Parser) step(b byte) (Event, bool) {
	switch p.state {
	case stGround:
		return p.ground(b)
	case stUTF8:
		if b&0xc0 != 0x80 {
			// cut short: report, then reread this byte from ground
			p.state = stGround
			p.pos--
			return p.keyEvent(Key{Rune: utf8.RuneError}, Mods{}), true
		}
		p.codepoint = p.codepoint<<6 | rune(b&0x3f)
		p.utf8Left--
		if p.utf8Left > 0 {
			return Event{}, false
		}
		p.state = stGround
		return p.keyEvent(Key{Rune: p.codepoint}, Mods{}), true
	case stEscape:
		switch b {
		case '[':
			p.record(b)
			p.state = stCSI
			p.paramCount = 0
			p.isSub = 0
			p.current = 0
			p.hasCurrent = false
			p.currentIsSub = false
			p.marker = 0
		case 'O':
			p.record(b)
			p.state = stSS3
		case ']', 'P', '_':
			// OSC, DCS and APC strings, up to BEL or ST
			p.record(b)
			p.state = stOSC
		case 0x1b:
			// two ESCs: the first was the Escape key
			p.startSequence()
			if p.inPaste {
				return Event{Kind: EventPaste, Bytes: escByte}, true
			}
			return p.keyEvent(Key{Named: Escape}, Mods{}), true
		default:
			// ESC then a key is Alt + that key
			p.state = stGround
			p.pos--
			if p.inPaste {
				return Event{Kind: EventPaste, Bytes: escByte}, true
			}
			p.alt = true
		}
		return Event{}, false
	case stCSI:
		p.record(b)
		switch {
		case b >= '0' && b <= '9':
			p.current = satAdd(satMul10(p.current), uint32(b-'0'))
			p.hasCurrent = true
		case b == ':':
			p.pushParam()
			p.currentIsSub = true
		case b == ';':
			p.pushParam()
			p.currentIsSub = false
		case b == '<' || b == '=' || b == '>' || b == '?':
			if p.sequenceLen == 3 {
				p.marker = b
			}
		case b >= 0x20 && b <= 0x2f:
		case b >= 0x40 && b <= 0x7e:
			if p.hasCurrent || p.paramCount > 0 {
				p.pushParam()
			}
			p.state = stGround
			return p.dispatchCSI(b)
		case b == 0x1b:
			p.startSequence()
		}
		return Event{}, false
	case stSS3:
		p.record(b)
		p.state = stGround
		if p.inPaste {
			return Event{Kind: EventPaste, Bytes: p.seq()}, true
		}
		var n Named
		switch b {
		case 'A':
			n = Up
		case 'B':
			n = Down
		case 'C':
			n = Right
		case 'D':
			n = Left
		case 'H':
			n = Home
		case 'F':
			n = End
		case 'P':
			n = F1
		case 'Q':
			n = F2
		case 'R':
			n = F3
		case 'S':
			n = F4
		}
		if n != NamedNone {
			return p.keyEvent(Key{Named: n}, Mods{}), true
		}
		return Event{Kind: EventUnknown, Bytes: p.seq()}, true
	case stOSC:
		p.record(b)
		switch b {
		case 0x07:
			return p.endOSC(), true
		case 0x1b:
			p.state = stOSCEscape
		}
		return Event{}, false
	case stOSCEscape:
		p.record(b)
		if b == '\\' {
			return p.endOSC(), true
		}
		p.state = stOSC
		return Event{}, false
	}
	return Event{}, false
}

func satMul10(v uint32) uint32 {
	if v > 0xffffffff/10 {
		return 0xffffffff
	}
	return v * 10
}

func satAdd(a, b uint32) uint32 {
	if a > 0xffffffff-b {
		return 0xffffffff
	}
	return a + b
}

func (p *Parser) ground(b byte) (Event, bool) {
	switch {
	case b == 0x1b:
		p.startSequence()
		return Event{}, false
	case b == 0x0d:
		return p.keyEvent(Key{Named: Enter}, Mods{}), true
	case b == 0x09:
		return p.keyEvent(Key{Named: Tab}, Mods{}), true
	case b == 0x7f:
		return p.keyEvent(Key{Named: Backspace}, Mods{}), true
	case b == 0x00:
		return p.keyEvent(Key{Rune: ' '}, Mods{Ctrl: true}), true
	case b <= 0x1a:
		return p.keyEvent(Key{Rune: rune('a' + b - 1)}, Mods{Ctrl: true}), true
	case b <= 0x1f:
		return p.keyEvent(Key{Rune: rune(`\]^_`[b-0x1c])}, Mods{Ctrl: true}), true
	case b <= 0x7e:
		return p.keyEvent(Key{Rune: rune(b)}, Mods{}), true
	case b >= 0xc0:
		var n int
		switch {
		case b < 0xe0:
			n = 2
		case b < 0xf0:
			n = 3
		case b < 0xf8:
			n = 4
		default:
			return p.keyEvent(Key{Rune: utf8.RuneError}, Mods{}), true
		}
		p.codepoint = rune(b & (0x7f >> n))
		p.utf8Left = n - 1
		p.state = stUTF8
		return Event{}, false
	default: // continuation byte with no start
		return p.keyEvent(Key{Rune: utf8.RuneError}, Mods{}), true
	}
}

func (p *Parser) dispatchCSI(final byte) (Event, bool) {
	sequence := p.seq()
	if p.inPaste {
		if p.marker == 0 && final == '~' && p.param(0, 0) == 201 {
			p.inPaste = false
			return Event{Kind: EventPasteEnd}, true
		}
		return Event{Kind: EventPaste, Bytes: sequence}, true
	}
	if p.marker == '<' && (final == 'M' || final == 'm') {
		return p.mouse(final == 'm'), true
	}
	// CSI ? 997 ; 1 n dark, ; 2 n light
	if p.marker == '?' && final == 'n' && p.param(0, 0) == 997 {
		if v := p.param(1, 0); v == 1 || v == 2 {
			return Event{Kind: EventDarkTheme, Flag: v == 1}, true
		}
	}
	if p.marker != 0 {
		// replies to queries: DA1 (?...c), DA2 (>...c), modes (?...$y), kitty flags (?...u)
		return Event{Kind: EventTermResponse, Bytes: sequence}, true
	}
	mods := ModsFromParam(p.param(1, 1))
	var n Named
	switch final {
	case 'A':
		n = Up
	case 'B':
		n = Down
	case 'C':
		n = Right
	case 'D':
		n = Left
	case 'H':
		n = Home
	case 'F':
		n = End
	case 'P':
		n = F1
	case 'Q':
		n = F2
	case 'R':
		n = F3
	case 'S':
		n = F4
	case '~':
		switch p.param(0, 0) {
		case 1, 7:
			n = Home
		case 2:
			n = Insert
		case 3:
			n = Delete
		case 4, 8:
			n = End
		case 5:
			n = PageUp
		case 6:
			n = PageDown
		case 11:
			n = F1
		case 12:
			n = F2
		case 13:
			n = F3
		case 14:
			n = F4
		case 15:
			n = F5
		case 17:
			n = F6
		case 18:
			n = F7
		case 19:
			n = F8
		case 20:
			n = F9
		case 21:
			n = F10
		case 23:
			n = F11
		case 24:
			n = F12
		case 200:
			p.inPaste = true
			return Event{Kind: EventPasteStart}, true
		}
	case 'Z':
		return p.keyEvent(Key{Named: Tab}, Mods{Shift: true}), true
	case 'I':
		return Event{Kind: EventFocus, Flag: true}, true
	case 'O':
		return Event{Kind: EventFocus, Flag: false}, true
	case 'u':
		return p.csiU()
	}
	if n != NamedNone {
		return p.keyEvent(Key{Named: n}, mods), true
	}
	return Event{Kind: EventUnknown, Bytes: sequence}, true
}

// csiU decodes a kitty key: ESC [ code : shifted ; mods : event u.
func (p *Parser) csiU() (Event, bool) {
	code := p.field(0, 0, 0)
	shifted := p.field(0, 1, 0)
	mods := ModsFromParam(p.field(1, 0, 1))
	// event type 3 is a release: drop it
	if p.field(1, 1, 1) == 3 {
		return Event{}, false
	}
	if k, ok := kittyKey(code); ok {
		return p.keyEvent(k, mods), true
	}
	// other private-use codes: F13+, media keys, modifier keys
	if (code >= 0xe000 && code <= 0xf8ff) || code > 0x10ffff {
		return Event{Kind: EventUnknown, Bytes: p.seq()}, true
	}
	// Shift without Ctrl/Super is just the shifted character (<M-!>);
	// with Ctrl keep base key and Shift (<C-S-a>).
	cp := rune(code)
	if mods.Shift && !mods.Ctrl && !mods.Super && shifted != 0 && shifted <= 0x10ffff {
		cp = rune(shifted)
		mods.Shift = false
	}
	return p.keyEvent(Key{Rune: cp}, mods), true
}

func kittyKey(code uint32) (Key, bool) {
	switch {
	case code == 13 || code == 57414:
		return Key{Named: Enter}, true
	case code == 9:
		return Key{Named: Tab}, true
	case code == 27:
		return Key{Named: Escape}, true
	case code == 127:
		return Key{Named: Backspace}, true
	case code >= 57399 && code <= 57408:
		return Key{Rune: rune('0' + code - 57399)}, true
	}
	switch code {
	case 57409:
		return Key{Rune: '.'}, true
	case 57410:
		return Key{Rune: '/'}, true
	case 57411:
		return Key{Rune: '*'}, true
	case 57412:
		return Key{Rune: '-'}, true
	case 57413:
		return Key{Rune: '+'}, true
	case 57415:
		return Key{Rune: '='}, true
	case 57416:
		return Key{Rune: ','}, true
	case 57417:
		return Key{Named: Left}, true
	case 57418:
		return Key{Named: Right}, true
	case 57419:
		return Key{Named: Up}, true
	case 57420:
		return Key{Named: Down}, true
	case 57421:
		return Key{Named: PageUp}, true
	case 57422:
		return Key{Named: PageDown}, true
	case 57423:
		return Key{Named: Home}, true
	case 57424:
		return Key{Named: End}, true
	case 57425:
		return Key{Named: Insert}, true
	case 57426:
		return Key{Named: Delete}, true
	}
	return Key{}, false
}

// mouse decodes ESC [ < button ; col ; row M|m. Columns and rows count from 1.
func (p *Parser) mouse(release bool) Event {
	cb := p.param(0, 0)
	col := uint16(min(p.param(1, 1)-1, 0xffff))
	row := uint16(min(p.param(2, 1)-1, 0xffff))
	mods := Mods{Shift: cb&4 != 0, Alt: cb&8 != 0, Ctrl: cb&16 != 0}
	low := cb & 3
	if cb&64 != 0 {
		return Event{Kind: EventMouse, Mouse: Mouse{Button: WheelUp + MouseButton(low), Action: Press, Mods: mods, Row: row, Col: col}}
	}
	button := MouseButton(low)
	action := Press
	switch {
	case release:
		action = Release
	case cb&32 != 0:
		if button == MouseNone {
			action = Motion
		} else {
			action = Drag
		}
	}
	return Event{Kind: EventMouse, Mouse: Mouse{Button: button, Action: action, Mods: mods, Row: row, Col: col}}
}

func (p *Parser) endOSC() Event {
	p.state = stGround
	if p.inPaste {
		return Event{Kind: EventPaste, Bytes: p.seq()}
	}
	return Event{Kind: EventTermResponse, Bytes: p.seq()}
}

func (p *Parser) param(i int, def uint32) uint32 { return p.field(i, 0, def) }

func (p *Parser) sub(i int) bool { return p.isSub&(1<<i) != 0 }

// field is sub-parameter sub of parameter group, or def when missing or 0.
func (p *Parser) field(group, sub int, def uint32) uint32 {
	g, start := 0, 0
	for ; start < p.paramCount; start++ {
		if start > 0 && !p.sub(start) {
			g++
		}
		if g == group {
			break
		}
	}
	if start >= p.paramCount {
		return def
	}
	i := start + sub
	if i >= p.paramCount {
		return def
	}
	for j := start + 1; j <= i; j++ {
		if !p.sub(j) {
			return def
		}
	}
	if p.params[i] == 0 {
		return def
	}
	return p.params[i]
}

func (p *Parser) pushParam() {
	if p.paramCount < maxParams {
		p.params[p.paramCount] = p.current
		if p.currentIsSub {
			p.isSub |= 1 << p.paramCount
		} else {
			p.isSub &^= 1 << p.paramCount
		}
		p.paramCount++
	}
	p.current = 0
	p.hasCurrent = false
}

func (p *Parser) startSequence() {
	p.state = stEscape
	p.sequence[0] = 0x1b
	p.sequenceLen = 1
}

func (p *Parser) record(b byte) {
	if p.sequenceLen < maxSequence {
		p.sequence[p.sequenceLen] = b
		p.sequenceLen++
	}
}

func (p *Parser) keyEvent(k Key, m Mods) Event {
	if p.alt {
		m.Alt = true
		p.alt = false
	}
	return Event{Kind: EventKey, Key: KeyEvent{Key: k, Mods: m}}
}
