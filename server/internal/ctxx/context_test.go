package ctxx_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/ctxx"
)

func TestDetachUsesWorkLifetime(t *testing.T) {
	type key struct{}
	request, disconnect := context.WithTimeout(context.WithValue(context.Background(), key{}, "kept"), time.Hour)
	defer disconnect()
	job, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	work := ctxx.Detach(ctxx.WithParent(request, job))
	disconnect()
	if work.Err() != nil || context.Cause(work) != nil || work.Value(key{}) != "kept" {
		t.Fatal("disconnect leaked or values lost")
	}
	if _, ok := work.Deadline(); ok {
		t.Fatal("request deadline retained")
	}
	shutdown()
	if !errors.Is(work.Err(), context.Canceled) || !errors.Is(context.Cause(work), context.Canceled) {
		t.Fatal("shutdown lost")
	}
}

func TestUninterruptibleMasksParentAcrossDetach(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(map[bool]string{false: "batch", true: "nested-detach"}[nested], func(t *testing.T) {
			type key struct{}
			job, shutdown := context.WithCancel(context.Background())
			request, disconnect := context.WithCancel(context.WithValue(context.Background(), key{}, "kept"))
			batch := ctxx.Uninterruptible(ctxx.WithParent(request, job))
			if nested {
				batch = ctxx.Detach(ctxx.Detach(batch))
			}
			disconnect()
			shutdown()
			if batch.Done() != nil || batch.Err() != nil || context.Cause(batch) != nil || batch.Value(key{}) != "kept" {
				t.Fatal("batch restored cancellation or lost values")
			}
			child, cancel := context.WithCancel(batch)
			if child.Err() != nil {
				t.Fatal(child.Err())
			}
			cancel()
			if !errors.Is(child.Err(), context.Canceled) {
				t.Fatal("child's own cancellation lost")
			}
		})
	}
}

func TestDetachOutsideRuntime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if work := ctxx.Detach(ctx); work.Done() != nil || work.Err() != nil || context.Cause(work) != nil {
		t.Fatal("plain request cancellation retained")
	}
}
