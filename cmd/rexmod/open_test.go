package main

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSocketDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", "")
	rt := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", rt)
	want := filepath.Join(rt, "rexmod")
	if runtime.GOOS == "darwin" {
		want = cacheDir() // the Mac keeps its sockets where they were
	}
	if got := socketDir(); got != want {
		t.Errorf("with XDG_RUNTIME_DIR: %q, want %q", got, want)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got := socketDir(); got != cacheDir() {
		t.Errorf("without XDG_RUNTIME_DIR: %q, want the cache directory", got)
	}
	t.Setenv("XDG_RUNTIME_DIR", "relative/dir")
	if got := socketDir(); got != cacheDir() {
		t.Errorf("relative XDG_RUNTIME_DIR: %q", got)
	}
}

func TestRexmodDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	if got := rexmodDataDir(); got != filepath.Join(home, ".local", "share", "rexmod") {
		t.Errorf("default: %q", got)
	}
	t.Setenv("XDG_DATA_HOME", "/data")
	if got := rexmodDataDir(); got != "/data/rexmod" {
		t.Errorf("XDG_DATA_HOME: %q", got)
	}
}
