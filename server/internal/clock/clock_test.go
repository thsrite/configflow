package clock_test

import (
	"testing"
	"time"

	"configflow/internal/clock"
)

func TestRealClockTimers(t *testing.T) {
	c := clock.Real{}
	before := time.Now()
	if now := c.Now(); now.Before(before) || now.After(time.Now()) {
		t.Fatal(now)
	}
	stopped := c.NewTimer(time.Hour)
	if !stopped.Stop() || stopped.Stop() {
		t.Fatal("Stop result")
	}
	select {
	case <-stopped.C():
		t.Fatal("stopped timer fired")
	default:
	}
	timer := c.NewTimer(0)
	defer timer.Stop()
	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("immediate timer did not fire")
	}
}
