package lifecycletest

import (
	"testing"
	"time"
)

func TestClockTimers(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewClock(start)
	a, b := c.NewTimer(5*time.Second), c.NewTimer(10*time.Second)
	c.Advance(4 * time.Second)
	select {
	case <-a.C():
		t.Fatal("timer fired early")
	default:
	}
	c.Advance(time.Second)
	if at := <-a.C(); !at.Equal(start.Add(5 * time.Second)) {
		t.Fatal(at)
	}
	if a.Stop() || !b.Stop() || b.Stop() || c.Pending() != 0 {
		t.Fatal("timer stop semantics")
	}
	c.Advance(time.Hour)
	select {
	case <-b.C():
		t.Fatal("stopped timer fired")
	default:
	}
	if at := <-c.NewTimer(0).C(); at != c.Now() {
		t.Fatal(at)
	}
}
