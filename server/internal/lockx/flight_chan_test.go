package lockx

import (
	"context"
	"errors"
	"runtime"
	"testing"
)

func TestFlightChannelsShareWorkWithoutWaiterGoroutines(t *testing.T) {
	s := newTestSpawner(t)
	f := NewFlight[string, int](s, "channel-work")
	workGate, finish := gate(t)
	started := make(chan struct{})
	firstCtx, cancel := context.WithCancel(context.Background())
	first := f.DoChan(firstCtx, "one", func(ctx context.Context) (int, error) {
		close(started)
		<-workGate
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		return 42, nil
	})
	await(t, started)
	baseline := runtime.NumGoroutine()
	cancel()
	channels := []<-chan Result[int]{first}
	for range 1000 {
		channels = append(channels, f.DoChan(context.Background(), "one", func(context.Context) (int, error) {
			t.Error("joined work ran")
			return -1, nil
		}))
	}
	if got := runtime.NumGoroutine(); got > baseline+5 {
		t.Fatalf("waiters started goroutines: %d -> %d", baseline, got)
	}
	doResult := make(chan flightResult, 1)
	go func() {
		v, err, shared := f.Do(context.Background(), "one", func(context.Context) (int, error) {
			t.Error("Do did not join DoChan")
			return -1, nil
		})
		doResult <- flightResult{v, err, shared}
	}()
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.calls["one"].joiners == 1001 })
	finish()
	// Leave the first waiter's channel unread while everyone else receives.
	for _, ch := range channels[1:] {
		if r := await(t, ch); r.Value != 42 || r.Err != nil || !r.Shared {
			t.Fatal(r)
		}
	}
	if r := await(t, first); r.Value != 42 || r.Err != nil || !r.Shared {
		t.Fatal(r)
	}
	if r := await(t, doResult); r.value != 42 || r.err != nil || !r.shared {
		t.Fatal(r)
	}
	if s.calls.Load() != 1 {
		t.Fatal(s.calls.Load())
	}
}

func TestFlightChannelPanicWakesAllWaiters(t *testing.T) {
	f := NewFlight[string, int](newTestSpawner(t), "panic-channel")
	workGate, finish := gate(t)
	a := f.DoChan(context.Background(), "one", func(context.Context) (int, error) { <-workGate; panic("private") })
	b := f.DoChan(context.Background(), "one", nil)
	finish()
	for _, ch := range []<-chan Result[int]{a, b} {
		if r := await(t, ch); !errors.Is(r.Err, ErrPanic) || !r.Shared {
			t.Fatal(r)
		}
	}
	if r := await(t, f.DoChan(context.Background(), "one", func(context.Context) (int, error) { return 2, nil })); r.Value != 2 || r.Err != nil || r.Shared {
		t.Fatal(r)
	}
}

type blockedRefusal struct{ entered, release chan struct{} }

func (s blockedRefusal) Spawn(context.Context, string, func(context.Context)) error {
	close(s.entered)
	<-s.release
	return errMissingSpawner
}

func TestFlightChannelRefusalWakesAllWaiters(t *testing.T) {
	s := blockedRefusal{make(chan struct{}), make(chan struct{})}
	f := NewFlight[string, int](s, "refused-channel")
	first := make(chan (<-chan Result[int]), 1)
	go func() { first <- f.DoChan(context.Background(), "one", nil) }()
	await(t, s.entered)
	second := f.DoChan(context.Background(), "one", nil)
	close(s.release)
	for _, ch := range []<-chan Result[int]{await(t, first), second} {
		if r := await(t, ch); !errors.Is(r.Err, errMissingSpawner) || !r.Shared {
			t.Fatal(r)
		}
	}
	var zero Flight[string, int]
	if r := await(t, zero.DoChan(context.Background(), "one", nil)); !errors.Is(r.Err, errMissingSpawner) {
		t.Fatal(r)
	}
}
