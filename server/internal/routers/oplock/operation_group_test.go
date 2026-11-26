package oplock

import (
	"configflow/internal/routers/spec"
	"context"
	"errors"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"testing"
	"time"

	"configflow/internal/lockx"
)

func TestOperationKeys(t *testing.T) {
	a := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://router.example/"}}
	b := &spec.Router{RouterSettings: spec.RouterSettings{Type: "ikuai", Endpoint: "https://other.example"}}
	clone := *a
	clone.Endpoint = "https://router.example"
	want := [][2]string{{"ikuai", b.Endpoint}, {"routeros", clone.Endpoint}}
	for _, rows := range [][]*spec.Router{{a, b, &clone, nil}, {&clone, b, a}} {
		if got := operationKeys(rows); !reflect.DeepEqual(got, want) {
			t.Fatalf("keys=%v", got)
		}
	}
}

func TestRouterOperationGroupCancellation(t *testing.T) {
	a := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://a.example"}}
	b := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://b.example"}}
	_, unlock, err := RouterOperationLock(context.Background(), *b, false)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, release, err := RouterOperationLocks(ctx, []*spec.Router{b, a}, false)
		if release != nil {
			release()
		}
		done <- err
	}()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !routeLocks.Held(operationKeys([]*spec.Router{a})[0]) {
		select {
		case <-deadline.C:
			t.Fatal("first lock not held")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if routeLocks.Held(operationKeys([]*spec.Router{a})[0]) {
		t.Fatal("partial group leaked")
	}
}

func TestRouterOperationGroupOppositeOrder(t *testing.T) {
	a := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://a.example"}}
	b := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://b.example"}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	before := lockx.Violations()
	var wg sync.WaitGroup
	for _, rows := range [][]*spec.Router{{a, b}, {b, a}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 30 {
				_, release, err := RouterOperationLocks(ctx, rows, false)
				if err != nil {
					t.Error(err)
					return
				}
				release()
			}
		}()
	}
	wg.Wait()
	if delta := lockx.Violations() - before; delta != 0 {
		t.Fatalf("group violations=%d", delta)
	}
}

func TestRouterOperationGroupNestingNegativeControl(t *testing.T) {
	if os.Getenv("CONFIGFLOW_GROUP_LOCK_NEGATIVE") == "" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestRouterOperationGroupNestingNegativeControl$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_GROUP_LOCK_NEGATIVE=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("negative control: %v %s", err, output)
		}
		return
	}
	// Isolate the deliberate violation so normal publication tests keep asserting zero.
	a := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://a.example"}}
	b := &spec.Router{RouterSettings: spec.RouterSettings{Type: "routeros", Endpoint: "https://b.example"}}
	held, release, err := RouterOperationLocks(t.Context(), []*spec.Router{a}, false)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	before := lockx.Violations()
	_, unlock, err := RouterOperationLock(held, *b, false)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
	if lockx.Violations()-before != 1 {
		t.Fatal("outside nesting escaped checker")
	}
}
