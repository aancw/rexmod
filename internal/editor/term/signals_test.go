package term

import (
	"syscall"
	"testing"
	"time"
)

func TestRaiseThenDrain(t *testing.T) {
	s := NewSignals(syscall.SIGWINCH)
	defer s.Stop()
	syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
	syscall.Kill(syscall.Getpid(), syscall.SIGWINCH)
	var fired SignalSet
	select {
	case sig := <-s.C:
		fired.Add(sig)
	case <-time.After(2 * time.Second):
		t.Fatal("no signal")
	}
	fired |= s.Drain()
	if !fired.Contains(syscall.SIGWINCH) || fired.Contains(syscall.SIGTERM) {
		t.Fatalf("fired = %b", fired)
	}
}

func TestEmptyDrain(t *testing.T) {
	s := NewSignals(syscall.SIGWINCH)
	defer s.Stop()
	if got := s.Drain(); got != 0 {
		t.Fatalf("got %b", got)
	}
}
