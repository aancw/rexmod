package nvim

// Ported from neorex (MIT): src/nvim/Forwarder.zig.

import (
	"strings"

	"github.com/alpcanaydin/rexmod/internal/editor/term"
)

// Forwarder turns terminal input into Neovim API calls: keys, pastes,
// focus, mouse and the terminal's theme.
type Forwarder struct {
	parser term.Parser
	// The next nvim_paste phase: 1 starts a paste, 2 continues it.
	pastePhase int64
	// For debug commands; the real editor gives Ctrl+Q to Neovim.
	CtrlQQuits bool
	// The grid mouse events land on; 0 without ext_multigrid.
	Grid uint32
	// Floats drawn over the terminal, lowest first: mouse events on one go to its grid.
	targets []Target
	// The float a button went down on: its drags and release go there too.
	grab *Target
	// Off when the caller tells Neovim about focus itself, from Focused.
	SendFocus bool
	// The last focus report seen; nil before any.
	Focused *bool
}

// Target is a float's grid and where it's drawn, in terminal cells.
type Target struct {
	Grid          uint32
	Row, Col      int32
	Width, Height uint32
}

const MaxTargets = 16

// NewForwarder returns a forwarder that sends focus and gives Ctrl+Q to Neovim.
func NewForwarder() *Forwarder {
	return &Forwarder{pastePhase: 1, SendFocus: true}
}

func (f *Forwarder) SetTargets(ts []Target) {
	if len(ts) > MaxTargets {
		ts = ts[:MaxTargets]
	}
	f.targets = append(f.targets[:0], ts...)
}

type routed struct {
	grid     uint32
	row, col uint16
}

func clampU16(v int64) uint16 {
	if v < 0 {
		return 0
	}
	if v > 0xffff {
		return 0xffff
	}
	return uint16(v)
}

// route is the grid a mouse event lands on, and where on it.
func (f *Forwarder) route(m term.Mouse) routed {
	held := m.Action == term.Drag || m.Action == term.Release
	var hit *Target
	if held && f.grab != nil {
		hit = f.grab
	} else {
		for j := len(f.targets) - 1; j >= 0; j-- {
			t := f.targets[j]
			if int64(m.Row) >= int64(t.Row) && int64(m.Row) < int64(t.Row)+int64(t.Height) &&
				int64(m.Col) >= int64(t.Col) && int64(m.Col) < int64(t.Col)+int64(t.Width) {
				tc := t
				hit = &tc
				break
			}
		}
	}
	if m.Action == term.Press && m.Button != term.WheelUp && m.Button != term.WheelDown {
		f.grab = hit
	}
	if m.Action == term.Release {
		f.grab = nil
	}
	if hit == nil {
		return routed{f.Grid, m.Row, m.Col}
	}
	return routed{hit.Grid, clampU16(int64(m.Row) - int64(hit.Row)), clampU16(int64(m.Col) - int64(hit.Col))}
}

// Feed sends the input in b to Neovim and flushes. False if the user
// pressed Ctrl+Q to quit and CtrlQQuits is set.
func (f *Forwarder) Feed(c *Client, b []byte) (bool, error) {
	if f.pastePhase == 0 {
		f.pastePhase = 1
	}
	var keys strings.Builder
	send := func() error {
		if keys.Len() == 0 {
			return nil
		}
		_, err := c.Request("nvim_input", keys.String())
		keys.Reset()
		return err
	}
	f.parser.Feed(b)
	for {
		ev, ok := f.parser.Next()
		if !ok {
			break
		}
		var err error
		switch ev.Kind {
		case term.EventKey:
			if f.CtrlQQuits && isQuit(ev.Key) {
				return false, nil
			}
			keys.WriteString(ev.Key.Nvim())
		case term.EventPasteStart:
			f.pastePhase = 1
		case term.EventPaste:
			if err = send(); err == nil {
				err = paste(c, ev.Bytes, f.pastePhase)
				f.pastePhase = 2
			}
		case term.EventPasteEnd:
			if f.pastePhase == 2 {
				err = paste(c, nil, 3)
			}
		case term.EventFocus:
			v := ev.Flag
			f.Focused = &v
			if f.SendFocus {
				_, err = c.Request("nvim_ui_set_focus", v)
			}
		case term.EventMouse:
			if a, ok := mouseArgsFrom(ev.Mouse); ok {
				if err = send(); err == nil {
					to := f.route(ev.Mouse)
					_, err = c.Request("nvim_input_mouse", a.button, a.action, a.mods, int64(to.grid), int64(to.row), int64(to.col))
				}
			}
		case term.EventTermResponse:
			// as Neovim's own TUI does: plugins that queried the terminal
			// through nvim_ui_send hear back in TermResponse
			if err = send(); err == nil {
				_, err = c.Request("nvim_ui_term_event", "termresponse", string(ev.Bytes))
			}
		case term.EventDarkTheme:
			bg := "light"
			if ev.Flag {
				bg = "dark"
			}
			_, err = c.ExecLua("if vim.g.neorex_follow_theme ~= false and vim.g.rexmod_follow_theme ~= false then vim.o.background = ... end", bg)
		}
		if err != nil {
			return true, err
		}
	}
	if err := send(); err != nil {
		return true, err
	}
	return true, c.Flush()
}

// paste is one piece of a streamed nvim_paste. Terminals send newlines as CR.
func paste(c *Client, text []byte, phase int64) error {
	s := strings.ReplaceAll(string(text), "\r", "\n")
	_, err := c.Request("nvim_paste", s, true, phase)
	return err
}

type mouseArgs struct{ button, action, mods string }

// mouseArgsFrom gives nvim_input_mouse's arguments; false for events Neovim has no name for.
func mouseArgsFrom(m term.Mouse) (mouseArgs, bool) {
	var a mouseArgs
	switch m.Button {
	case term.MouseLeft, term.MouseMiddle, term.MouseRight:
		a.button = map[term.MouseButton]string{term.MouseLeft: "left", term.MouseMiddle: "middle", term.MouseRight: "right"}[m.Button]
		switch m.Action {
		case term.Press:
			a.action = "press"
		case term.Drag:
			a.action = "drag"
		case term.Release:
			a.action = "release"
		default:
			return a, false
		}
	case term.WheelUp:
		a.button, a.action = "wheel", "up"
	case term.WheelDown:
		a.button, a.action = "wheel", "down"
	case term.WheelLeft:
		a.button, a.action = "wheel", "left"
	case term.WheelRight:
		a.button, a.action = "wheel", "right"
	default:
		if m.Action != term.Motion {
			return a, false
		}
		a.button = "move"
	}
	// as in key names: C-A-S-D-
	if m.Mods.Ctrl {
		a.mods += "C-"
	}
	if m.Mods.Alt {
		a.mods += "A-"
	}
	if m.Mods.Shift {
		a.mods += "S-"
	}
	if m.Mods.Super {
		a.mods += "D-"
	}
	return a, true
}

func isQuit(k term.KeyEvent) bool {
	return k.Mods.Ctrl && !k.Mods.Alt && k.Key.Named == term.NamedNone && k.Key.Rune == 'q'
}
