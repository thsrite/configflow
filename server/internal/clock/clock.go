// Package clock supplies the shared time and timer interfaces used by scheduling
// and caches. It does not own work or spawn goroutines; lifecycle tracks work.
package clock

import "time"

// Timer is a stoppable one-shot timer supplied by a Clock.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
}

// Clock supplies time and timers. Tests can inject a manually advanced clock.
type Clock interface {
	Now() time.Time
	NewTimer(time.Duration) Timer
}

// Real uses the process clock and standard-library timers. Its zero value works.
type Real struct{}

// Now returns the current time, including its monotonic clock reading.
func (Real) Now() time.Time { return time.Now() }

// NewTimer returns a stoppable timer that fires after d.
func (Real) NewTimer(d time.Duration) Timer { return realTimer{time.NewTimer(d)} }

type realTimer struct{ timer *time.Timer }

func (t realTimer) C() <-chan time.Time { return t.timer.C }
func (t realTimer) Stop() bool          { return t.timer.Stop() }
