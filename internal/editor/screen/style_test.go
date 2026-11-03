package screen

import "testing"

func TestBlendOver(t *testing.T) {
	s := Style{DefaultFg: RGB(0xffffff), DefaultBg: RGB(0)}
	s.Attrs = []Attr{
		{},
		{Bg: RGB(0), Blend: 60},
		{Fg: RGB(0x6464c8), Bg: RGB(0xc86464)},
		{Bg: RGB(0x0000ff)},
		{Fg: RGB(0xaaaaaa)},
	}
	// no blend: unchanged
	eq(t, s.BlendOver(3, 2, false), 3)

	through := s.BlendOver(1, 2, true)
	if through < BlendBase {
		t.Fatal("not a blended id")
	}
	a := s.Attr(through)
	eq(t, a.Fg, RGB(0x3c3c78))
	eq(t, a.Bg, RGB(0x783c3c))
	eq(t, a.Blend, 0)
	// the same pair again: the same id
	eq(t, s.BlendOver(1, 2, true), through)

	solid := s.Attr(s.BlendOver(1, 2, false))
	eq(t, solid.Fg, Color{})
	eq(t, solid.Bg, RGB(0x783c3c))

	s.ClearBlends()
	eq(t, len(s.blended), 0)

	// a transparent background: text still dims, the background stays clear
	s.DefaultBg = Color{}
	clear := s.Attr(s.BlendOver(1, 4, true))
	eq(t, clear.Fg, RGB(0x666666))
	eq(t, clear.Bg, Color{})
}
