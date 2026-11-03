package msgpack

import (
	"bytes"
	"errors"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

func enc(t *testing.T, v any) []byte {
	t.Helper()
	b, err := Append(nil, v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNilAndBool(t *testing.T) {
	got := enc(t, []any{nil, true, false})[1:]
	if !bytes.Equal(got, []byte{0xc0, 0xc3, 0xc2}) {
		t.Fatalf("% x", got)
	}
}

func TestIntBoundaries(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "\x00"}, {127, "\x7f"}, {128, "\xcc\x80"}, {255, "\xcc\xff"},
		{256, "\xcd\x01\x00"}, {65535, "\xcd\xff\xff"},
		{65536, "\xce\x00\x01\x00\x00"}, {4294967295, "\xce\xff\xff\xff\xff"},
		{4294967296, "\xcf\x00\x00\x00\x01\x00\x00\x00\x00"},
		{math.MaxInt64, "\xcf\x7f\xff\xff\xff\xff\xff\xff\xff"},
		{-1, "\xff"}, {-32, "\xe0"}, {-33, "\xd0\xdf"}, {-128, "\xd0\x80"},
		{-129, "\xd1\xff\x7f"}, {-32768, "\xd1\x80\x00"},
		{-32769, "\xd2\xff\xff\x7f\xff"}, {math.MinInt32, "\xd2\x80\x00\x00\x00"},
		{math.MinInt32 - 1, "\xd3\xff\xff\xff\xff\x7f\xff\xff\xff"},
		{math.MinInt64, "\xd3\x80\x00\x00\x00\x00\x00\x00\x00"},
	}
	for _, c := range cases {
		if got := string(AppendInt(nil, c.n)); got != c.want {
			t.Errorf("%d: % x, want % x", c.n, got, c.want)
		}
		v, n, err := Decode([]byte(c.want))
		if err != nil || n != len(c.want) || v != c.n {
			t.Errorf("decode %d: %v %d %v", c.n, v, n, err)
		}
	}
}

func TestFloat(t *testing.T) {
	if got := enc(t, 1.5); !bytes.Equal(got, []byte{0xcb, 0x3f, 0xf8, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("% x", got)
	}
}

func TestStrBoundaries(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "\xa0"}, {31, "\xbf"}, {32, "\xd9\x20"}, {255, "\xd9\xff"},
		{256, "\xda\x01\x00"}, {65535, "\xda\xff\xff"}, {65536, "\xdb\x00\x01\x00\x00"},
	}
	for _, c := range cases {
		out := enc(t, strings.Repeat("x", c.n))
		if string(out[:len(c.want)]) != c.want || len(out) != len(c.want)+c.n {
			t.Errorf("%d: % x", c.n, out[:len(c.want)])
		}
	}
}

func TestArrayAndMapHeaderBoundaries(t *testing.T) {
	cases := []struct{ got, want string }{
		{string(AppendArrayHeader(nil, 15)), "\x9f"},
		{string(AppendArrayHeader(nil, 16)), "\xdc\x00\x10"},
		{string(AppendArrayHeader(nil, 65536)), "\xdd\x00\x01\x00\x00"},
		{string(AppendMapHeader(nil, 0)), "\x80"},
		{string(AppendMapHeader(nil, 15)), "\x8f"},
		{string(AppendMapHeader(nil, 16)), "\xde\x00\x10"},
		{string(AppendMapHeader(nil, 65536)), "\xdf\x00\x01\x00\x00"},
	}
	for i, c := range cases {
		if c.got != c.want {
			t.Errorf("%d: % x", i, c.got)
		}
	}
}

func TestAnRPCRequest(t *testing.T) {
	got := enc(t, []any{0, 1, "nvim_input", []string{"<C-a>"}})
	if string(got) != "\x94\x00\x01\xaanvim_input\x91\xa5<C-a>" {
		t.Fatalf("% x", got)
	}
}

func TestEncodeOtherTypes(t *testing.T) {
	got := enc(t, map[string]any{"b": uint32(2), "a": Raw{0xc0}})
	if string(got) != "\x82\xa1a\xc0\xa1b\x02" {
		t.Fatalf("% x", got)
	}
	if _, err := Append(nil, struct{}{}); err == nil {
		t.Fatal("want error")
	}
	var w bytes.Buffer
	if err := Encode(&w, Ext{1, []byte{5}}); err != nil || w.String() != "\xd4\x01\x05" {
		t.Fatalf("% x %v", w.Bytes(), err)
	}
}

func decode(t *testing.T, s string) any {
	t.Helper()
	v, n, err := Decode([]byte(s))
	if err != nil || n != len(s) {
		t.Fatalf("decode % x: %d %v", s, n, err)
	}
	return v
}

func TestDecodeScalars(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"\x93\xc0\xc2\xc3", []any{nil, false, true}},
		{"\x94\x00\x7f\xff\xe0", []any{int64(0), int64(127), int64(-1), int64(-32)}},
		{"\x94\xcc\x80\xcd\x01\x00\xd0\xdf\xd1\xff\x7f", []any{int64(128), int64(256), int64(-33), int64(-129)}},
		{"\xcf\x7f\xff\xff\xff\xff\xff\xff\xff", int64(math.MaxInt64)},
		{"\xcf\xff\xff\xff\xff\xff\xff\xff\xff", uint64(math.MaxUint64)},
		{"\xd3\x80\x00\x00\x00\x00\x00\x00\x00", int64(math.MinInt64)},
		{"\x92\xca\x3f\xc0\x00\x00\xcb\x3f\xf8\x00\x00\x00\x00\x00\x00", []any{1.5, 1.5}},
	}
	for _, c := range cases {
		if got := decode(t, c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("% x: %#v", c.in, got)
		}
	}
}

func TestDecodeContainersAndBlobs(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{"\x92\x01\x02", []any{int64(1), int64(2)}},
		{"\x81\xa1k\x01", Map{{"k", int64(1)}}},
		{"\xa0", ""},
		{"\xc4\x02ab", []byte("ab")},
		// Neovim sends handles as ext: buffer (0), window (1), tabpage (2)
		{"\xd4\x01\x05", Ext{1, []byte{5}}},
		{"\xc7\x02\x02\x01\x02", Ext{2, []byte{1, 2}}},
		// nvim_eval("1+2") reply
		{"\x94\x01\x01\xc0\x03", []any{int64(1), int64(1), nil, int64(3)}},
	}
	for _, c := range cases {
		if got := decode(t, c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("% x: %#v", c.in, got)
		}
	}
}

func TestDecodeCutShort(t *testing.T) {
	msg := "\x93\xcd\x01\x00\xa5hello\xcb\x3f\xf8\x00\x00\x00\x00\x00\x00"
	want := []any{int64(256), "hello", 1.5}
	if got := decode(t, msg); !reflect.DeepEqual(got, want) {
		t.Fatalf("%#v", got)
	}
	for i := range len(msg) {
		if _, _, err := Decode([]byte(msg[:i])); !errors.Is(err, ErrShort) {
			t.Fatalf("cut at %d: %v", i, err)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := []any{int64(-129), "nvim_input", nil, Map{}, Map{{int64(1), []any{true}}}, []byte{}, Ext{-1, []byte{1, 2, 3}}}
	if got := decode(t, string(enc(t, in))); !reflect.DeepEqual(got, in) {
		t.Fatalf("%#v", got)
	}
}

func TestDecodeRejectsC1(t *testing.T) {
	if _, _, err := Decode([]byte{0xc1}); !errors.Is(err, ErrInvalidByte) {
		t.Fatal(err)
	}
}

func TestHandle(t *testing.T) {
	// window 1000 as ext 1, a plain 7, -1 as fixext 1
	for _, c := range []struct {
		in   string
		want int64
	}{{"\xc7\x03\x01\xcd\x03\xe8", 1000}, {"\x07", 7}, {"\xd4\x01\xff", -1}} {
		if h, ok := Handle(decode(t, c.in)); !ok || h != c.want {
			t.Errorf("% x: %d %v", c.in, h, ok)
		}
	}
	if _, ok := Handle("x"); ok {
		t.Error("string is no handle")
	}
}

func expectNext(t *testing.T, f *Framer, want string, ok bool) {
	t.Helper()
	got, err := f.Next()
	if err != nil {
		t.Fatal(err)
	}
	if (got != nil) != ok || string(got) != want {
		t.Fatalf("got % x, want % x", got, want)
	}
}

func TestFramerNothingFed(t *testing.T) {
	var f Framer
	expectNext(t, &f, "", false)
}

func TestFramerTwoMessagesSplitAcrossFeeds(t *testing.T) {
	var f Framer
	f.Feed([]byte("\x94\x01\x01"))
	expectNext(t, &f, "", false)
	f.Feed([]byte("\xc0\x03\x94\x01"))
	expectNext(t, &f, "\x94\x01\x01\xc0\x03", true)
	expectNext(t, &f, "", false)
	f.Feed([]byte("\x01\xc0\x03"))
	expectNext(t, &f, "\x94\x01\x01\xc0\x03", true)
	expectNext(t, &f, "", false)
}

func TestFramerSplitInsideAString(t *testing.T) {
	var f Framer
	f.Feed([]byte("\x91\xa2h"))
	expectNext(t, &f, "", false)
	f.Feed([]byte("i"))
	expectNext(t, &f, "\x91\xa2hi", true)
}

func TestFramerSeveralMessagesInOneFeed(t *testing.T) {
	var f Framer
	f.Feed([]byte("\x93\x01\x91\xa2hi\xc0\x05\x90\x80"))
	expectNext(t, &f, "\x93\x01\x91\xa2hi\xc0", true)
	expectNext(t, &f, "\x05", true)
	expectNext(t, &f, "\x90", true)
	expectNext(t, &f, "\x80", true)
	expectNext(t, &f, "", false)
}

func TestFramerMapsOweKeysAndValues(t *testing.T) {
	var f Framer
	f.Feed([]byte("\x81\x01\x92\x02\x03\x04"))
	expectNext(t, &f, "\x81\x01\x92\x02\x03", true)
	expectNext(t, &f, "\x04", true)
}

func TestFramerLeftoverSurvivesCompaction(t *testing.T) {
	var f Framer
	f.Feed([]byte("\x05\x92\x01"))
	expectNext(t, &f, "\x05", true)
	expectNext(t, &f, "", false)
	f.Feed([]byte("\x02"))
	expectNext(t, &f, "\x92\x01\x02", true)
	// the handed-out 0x05 was dropped by the second feed
	if f.Buffered() != 3 {
		t.Fatal(f.Buffered())
	}
}

func TestFramerAPIInfoInRandomPieces(t *testing.T) {
	data, err := os.ReadFile("testdata/nvim_get_api_info.mpack")
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(1))
	for range 50 {
		var f Framer
		for off := 0; off < len(data); {
			n := min(len(data)-off, 1+r.Intn(500))
			f.Feed(data[off : off+n])
			off += n
			if off < len(data) {
				expectNext(t, &f, "", false)
			}
		}
		expectNext(t, &f, string(data), true)
		expectNext(t, &f, "", false)
	}
	if _, _, err := Decode(data); err != nil {
		t.Fatal(err)
	}
}

func TestFramerErrors(t *testing.T) {
	var f Framer
	f.Feed([]byte{0xc1})
	if _, err := f.Next(); !errors.Is(err, ErrInvalidByte) {
		t.Fatal(err)
	}
	var g Framer
	g.Feed(bytes.Repeat([]byte{0x91}, 40)) // [[[[...]]]]
	if _, err := g.Next(); !errors.Is(err, ErrTooDeep) {
		t.Fatal(err)
	}
}
