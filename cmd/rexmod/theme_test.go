package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestParseCustomThemes(t *testing.T) {
	data := []byte(`[{"id":"mine","palette":{"background":{"red":0,"green":0,"blue":0},
	  "foreground":{"red":1,"green":1,"blue":1},"selectionBackground":{"red":0.5,"green":0,"blue":1},
	  "selectionForeground":{"red":1,"green":0.5,"blue":0},"ansi":[{"red":1,"green":0,"blue":0}]}}]`)
	got := parseCustomThemes(data)["mine"]
	if got == nil {
		t.Fatal("no theme")
	}
	var src struct {
		Background, Foreground, SelectionBackground string
		Ansi                                        []string
	}
	if json.Unmarshal(got.Source, &src) != nil || src.Background != "#000000" || src.Foreground != "#ffffff" ||
		src.SelectionBackground != "#8000ff" || len(src.Ansi) != 1 || src.Ansi[0] != "#ff0000" {
		t.Errorf("source: %s", got.Source)
	}
}

func TestSaveRexTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	saveRexTheme("")
	saveRexTheme("not json")
	if _, err := os.Stat(rexThemePath()); err == nil {
		t.Fatal("saved something that isn't a theme")
	}
	saveRexTheme(`{"appearance":"light","light":{"id":"x","source":{}}}`)
	if a := savedAppearance(); a != "light" {
		t.Errorf("appearance %q", a)
	}
}
