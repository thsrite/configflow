package rulefetch

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle/lifecycletest"
)

type retryTestClock struct {
	clock.Clock
	delays chan time.Duration
}

func (c retryTestClock) NewTimer(d time.Duration) clock.Timer {
	timer := c.Clock.NewTimer(d)
	c.delays <- d
	return timer
}

func TestRetryUsesProductionDelaysAndCancellableClock(t *testing.T) {
	if DefaultClient.Sleep != nil {
		t.Fatal("production retry bypasses Clock")
	}
	for _, cancelWait := range []bool{false, true} {
		t.Run(map[bool]string{false: "advance", true: "cancel"}[cancelWait], func(t *testing.T) {
			fake := lifecycletest.NewClock(time.Unix(0, 0))
			delays := make(chan time.Duration, 2)
			c := &Client{Clock: retryTestClock{fake, delays}, Transport: &fakeTransport{reply: func(n int, _ TransportRequest) (*Response, error) {
				if n < 3 {
					return nil, &FetchError{Class: "ReadTimeout", Message: "synthetic", Transient: true}
				}
				return reply(200, "example.com"), nil
			}}}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				response, err := c.RequestRule(ctx, retrySource, Request{Config: sysConfig()})
				if response != nil {
					response.Close()
				}
				done <- err
			}()
			if d := <-delays; d != time.Second {
				t.Fatalf("first production retry=%s", d)
			}
			if cancelWait {
				cancel()
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				fake.Advance(time.Second)
				if d := <-delays; d != 2*time.Second {
					t.Fatalf("second production retry=%s", d)
				}
				fake.Advance(2 * time.Second)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
			}
			if fake.Pending() != 0 {
				t.Fatal("retry timer leaked")
			}
		})
	}
}
