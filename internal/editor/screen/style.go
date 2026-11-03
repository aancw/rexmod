package screen

import "math"

// Color is an optional 24-bit RGB color; unset means the default.
type Color struct {
	RGB uint32
	Set bool
}

// RGB is a set color.
func RGB(c uint32) Color { return Color{RGB: c & 0xffffff, Set: true} }

// PaletteIndex is an optional terminal palette color.
type PaletteIndex struct {
	I   uint8
	Set bool
}

// Idx is a set palette index.
func Idx(i uint8) PaletteIndex { return PaletteIndex{I: i, Set: true} }

// Underline is an underline style.
type Underline uint8

const (
	UnderlineNone Underline = iota
	UnderlineSingle
	UnderlineCurl
	UnderlineDouble
	UnderlineDotted
	UnderlineDashed
)

// Attr is a highlight from hl_attr_define. Unset colors mean the default.
type Attr struct {
	Fg, Bg, Sp Color
	// Palette indices from cterm_attr, kept only when the highlight has no
	// RGB color at all: a colorscheme that sets only ctermfg/ctermbg is
	// drawn in the terminal's own palette, so it follows the terminal's
	// theme.
	FgIdx, BgIdx                              PaletteIndex
	Bold, Italic, Reverse, Strikethrough, Dim bool
	Blink, Conceal, Overline, Altfont         bool
	Underline                                 Underline
	// Blend is 0 to 100.
	Blend uint8
}

// CursorShape is how the cursor is drawn.
type CursorShape uint8

const (
	CursorBlock CursorShape = iota
	CursorHorizontal
	CursorVertical
)

// CursorMode is a cursor look from mode_info_set.
type CursorMode struct {
	Shape CursorShape
	Blink bool
}

// BlendBase: highlight ids from here are blended highlights, never Neovim's.
const BlendBase uint32 = 1 << 31

const maxBlended = 512

type blended struct {
	front, back uint32
	through     bool
	attr        Attr
}

// Style is editor-wide drawing state every grid shares: highlights,
// default colors and how the cursor looks.
type Style struct {
	// Attrs by highlight id. Id 0 is the default.
	Attrs []Attr
	// Defaults for colors an Attr leaves unset.
	DefaultFg, DefaultBg, DefaultSp Color
	// CursorModes by mode, from mode_info_set.
	CursorModes []CursorMode
	// CursorStyleEnabled is false when 'guicursor' is empty: leave the
	// terminal's cursor alone.
	CursorStyleEnabled bool
	ModeIdx            uint32
	// Busy: between busy_start and busy_stop the cursor is hidden.
	Busy bool
	// Highlights made by blending a float over what's under it, with ids
	// from BlendBase. Good for one frame: cleared by ClearBlends.
	blended []blended
}

// Attr is highlight hl, or the default for an id never defined.
func (s *Style) Attr(hl uint32) Attr {
	if hl >= BlendBase {
		i := hl - BlendBase
		if int(i) < len(s.blended) {
			return s.blended[i].attr
		}
		return Attr{}
	}
	if int(hl) < len(s.Attrs) {
		return s.Attrs[hl]
	}
	return Attr{}
}

// ClearBlends forgets the blended highlights; call at the start of each frame.
func (s *Style) ClearBlends() { s.blended = s.blended[:0] }

// BlendOver is float highlight front over back, as Neovim's compositor
// blends them by front's blend. through: the float's cell is blank and
// back's text shows. Just front when it doesn't blend or colors are unknown.
func (s *Style) BlendOver(front, back uint32, through bool) uint32 {
	f := s.Attr(front)
	if f.Blend == 0 {
		return front
	}
	for i, b := range s.blended {
		if b.front == front && b.back == back && b.through == through {
			return BlendBase + uint32(i)
		}
	}
	if len(s.blended) == maxBlended {
		return front
	}
	b := s.Attr(back)
	fbg := orColor(f.Bg, s.DefaultBg)
	if !fbg.Set {
		return front
	}
	// unset on a transparent background: it stays transparent
	bbg := orColor(b.Bg, s.DefaultBg)
	a := f
	if through {
		a = b
		bfg := orColor(b.Fg, s.DefaultFg)
		if !bfg.Set {
			return front
		}
		a.Fg = RGB(mix(f.Blend, bfg.RGB, fbg.RGB))
		if b.Sp.Set {
			a.Sp = RGB(mix(f.Blend, b.Sp.RGB, fbg.RGB))
		}
	}
	if bbg.Set {
		a.Bg = RGB(mix(f.Blend, bbg.RGB, fbg.RGB))
	} else {
		a.Bg = Color{}
	}
	a.Blend = 0
	s.blended = append(s.blended, blended{front, back, through, a})
	return BlendBase + uint32(len(s.blended)-1)
}

func orColor(c, d Color) Color {
	if c.Set {
		return c
	}
	return d
}

// mix: ratio percent of back, the rest front, per channel.
func mix(ratio uint8, back, front uint32) uint32 {
	r := uint32(min(ratio, 100))
	var out uint32
	for _, shift := range []uint{16, 8, 0} {
		cb := (back >> shift) & 0xff
		cf := (front >> shift) & 0xff
		out |= ((r*cb + (100-r)*cf) / 100) << shift
	}
	return out
}

// CursorMode is how the cursor should look now; ok false to leave it alone.
func (s *Style) CursorMode() (CursorMode, bool) {
	if !s.CursorStyleEnabled || int(s.ModeIdx) >= len(s.CursorModes) {
		return CursorMode{}, false
	}
	return s.CursorModes[s.ModeIdx], true
}

// apply handles the style events; args is the event's arg tuple.
func (s *Style) apply(event string, args []any) error {
	switch event {
	case "hl_attr_define":
		if len(args) < 2 {
			return ErrBadMessage
		}
		id, err := expectU32(args[0])
		if err != nil {
			return err
		}
		a, err := readAttr(args[1])
		if err != nil {
			return err
		}
		if len(args) >= 3 {
			if err := readCtermColors(args[2], &a); err != nil {
				return err
			}
		}
		// A highlight with an RGB color belongs to an RGB colorscheme,
		// whose palette indices are only for 256-color terminals: the
		// default Visual is an RGB background with ctermfg=0 (black).
		if a.Fg.Set || a.Bg.Set || a.Sp.Set {
			a.FgIdx, a.BgIdx = PaletteIndex{}, PaletteIndex{}
		}
		for uint32(len(s.Attrs)) <= id {
			s.Attrs = append(s.Attrs, Attr{})
		}
		s.Attrs[id] = a
	case "default_colors_set":
		if len(args) < 3 {
			return ErrBadMessage
		}
		for i, dst := range []*Color{&s.DefaultFg, &s.DefaultBg, &s.DefaultSp} {
			c, err := expectColor(args[i])
			if err != nil {
				return err
			}
			*dst = c
		}
	case "mode_info_set":
		if len(args) < 2 {
			return ErrBadMessage
		}
		on, err := expectBool(args[0])
		if err != nil {
			return err
		}
		s.CursorStyleEnabled = on
		modes, err := expectArray(args[1])
		if err != nil {
			return err
		}
		s.CursorModes = s.CursorModes[:0]
		for _, m := range modes {
			cm, err := readCursorMode(m)
			if err != nil {
				return err
			}
			s.CursorModes = append(s.CursorModes, cm)
		}
	case "mode_change":
		if len(args) < 2 {
			return ErrBadMessage
		}
		idx, err := expectU32(args[1])
		if err != nil {
			return err
		}
		s.ModeIdx = idx
	case "busy_start", "busy_stop":
		s.Busy = event == "busy_start"
	}
	return nil
}

// readCursorMode: a mode_info_set entry. Only shape and blink are used.
func readCursorMode(v any) (CursorMode, error) {
	var m CursorMode
	var blinkon, blinkoff int64
	pairs, err := expectMap(v)
	if err != nil {
		return m, err
	}
	for _, p := range pairs {
		key, err := expectStr(p.key)
		if err != nil {
			return m, err
		}
		switch key {
		case "cursor_shape":
			s, err := expectStr(p.val)
			if err != nil {
				return m, err
			}
			switch s {
			case "horizontal":
				m.Shape = CursorHorizontal
			case "vertical":
				m.Shape = CursorVertical
			default:
				m.Shape = CursorBlock
			}
		case "blinkon":
			if blinkon, err = expectInt(p.val); err != nil {
				return m, err
			}
		case "blinkoff":
			if blinkoff, err = expectInt(p.val); err != nil {
				return m, err
			}
		}
	}
	// as Neovim's TUI decides
	m.Blink = blinkon != 0 && blinkoff != 0
	return m, nil
}

// readAttr: an rgb_attr map. Unknown keys, such as url, are skipped.
func readAttr(v any) (Attr, error) {
	var a Attr
	pairs, err := expectMap(v)
	if err != nil {
		return a, err
	}
	for _, p := range pairs {
		key, err := expectStr(p.key)
		if err != nil {
			return a, err
		}
		var color *Color
		var flag *bool
		var ul Underline
		switch key {
		case "foreground":
			color = &a.Fg
		case "background":
			color = &a.Bg
		case "special":
			color = &a.Sp
		case "blend":
			n, ok := toInt(p.val)
			if !ok || n < 0 || n > math.MaxUint8 {
				return a, ErrBadMessage
			}
			a.Blend = uint8(n)
			continue
		case "bold":
			flag = &a.Bold
		case "italic":
			flag = &a.Italic
		case "reverse":
			flag = &a.Reverse
		case "strikethrough":
			flag = &a.Strikethrough
		case "dim":
			flag = &a.Dim
		case "blink":
			flag = &a.Blink
		case "conceal":
			flag = &a.Conceal
		case "overline":
			flag = &a.Overline
		case "altfont":
			flag = &a.Altfont
		case "underline":
			ul = UnderlineSingle
		case "undercurl":
			ul = UnderlineCurl
		case "underdouble":
			ul = UnderlineDouble
		case "underdotted":
			ul = UnderlineDotted
		case "underdashed":
			ul = UnderlineDashed
		default:
			continue
		}
		switch {
		case color != nil:
			if *color, err = expectColor(p.val); err != nil {
				return a, err
			}
		case flag != nil:
			if *flag, err = expectBool(p.val); err != nil {
				return a, err
			}
		default:
			on, err := expectBool(p.val)
			if err != nil {
				return a, err
			}
			if on {
				a.Underline = ul
			}
		}
	}
	return a, nil
}

// readCtermColors: the palette colors from a cterm_attr map, into a. Its
// other keys repeat the RGB ones and are skipped.
func readCtermColors(v any, a *Attr) error {
	pairs, err := expectMap(v)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		key, err := expectStr(p.key)
		if err != nil {
			return err
		}
		var idx *PaletteIndex
		switch key {
		case "foreground":
			idx = &a.FgIdx
		case "background":
			idx = &a.BgIdx
		default:
			continue
		}
		n, err := expectInt(p.val)
		if err != nil {
			return err
		}
		if n >= 0 && n <= math.MaxUint8 {
			*idx = Idx(uint8(n))
		} else {
			*idx = PaletteIndex{}
		}
	}
	return nil
}

// expectColor: an RGB color, or unset for -1.
func expectColor(v any) (Color, error) {
	n, err := expectInt(v)
	if err != nil {
		return Color{}, err
	}
	if n == -1 {
		return Color{}, nil
	}
	if n < 0 || n > 0xffffff {
		return Color{}, ErrBadMessage
	}
	return RGB(uint32(n)), nil
}
