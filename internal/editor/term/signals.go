package term

import (
	"os"
	"os/signal"
	"syscall"
)

// Signals turns Unix signals into channel events (the Go version of the
// self-pipe trick).
type Signals struct {
	// C receives each signal as it fires. Select on it, then Drain.
	C chan os.Signal
}

// NewSignals starts catching sigs.
func NewSignals(sigs ...os.Signal) *Signals {
	s := &Signals{C: make(chan os.Signal, 32)}
	signal.Notify(s.C, sigs...)
	return s
}

// Stop restores the default handling.
func (s *Signals) Stop() { signal.Stop(s.C) }

// Drain empties the channel without blocking and returns which signals fired.
func (s *Signals) Drain() SignalSet {
	var set SignalSet
	for {
		select {
		case sig := <-s.C:
			set.Add(sig)
		default:
			return set
		}
	}
}

// SignalSet is which signals (numbers below 32) fired.
type SignalSet uint32

// Add puts sig in the set.
func (s *SignalSet) Add(sig os.Signal) {
	if n, ok := sig.(syscall.Signal); ok && n >= 0 && n < 32 {
		*s |= 1 << uint(n)
	}
}

// Contains reports whether sig is in the set.
func (s SignalSet) Contains(sig syscall.Signal) bool {
	return sig >= 0 && sig < 32 && s&(1<<uint(sig)) != 0
}
