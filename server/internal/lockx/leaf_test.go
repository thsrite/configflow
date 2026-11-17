package lockx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"
)

func TestLeafKeyedWaitsWithoutRecordingLevels(t *testing.T) {
	keys := NewLeafKeyed[string]("dashboard-state")
	ctx, releaseRepo := lockMutex(t, NewMutex(LevelRepository, "repository"), context.Background())
	defer releaseRepo()
	held, release, err := keys.Lock(ctx, "state", time.Second)
	if err != nil || held != ctx || !keys.Held("state") {
		t.Fatal("leaf changed context or acquisition", err)
	}
	_, _, err = keys.Lock(context.Background(), "state", 10*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatal(err)
	}
	wait, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { _, _, err := keys.Lock(wait, "state", time.Hour); result <- err }()
	cancel()
	if err := await(t, result); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	release()
	keys.mu.Lock()
	remaining := len(keys.entries)
	keys.mu.Unlock()
	if remaining != 0 {
		t.Fatal("unused leaf entries retained", remaining)
	}
	if Violations() != 0 {
		t.Fatal("leaf incorrectly checked levels")
	}
}

func TestLeafKeyedSerializesAndCleansDistinctKeys(t *testing.T) {
	keys := NewLeafKeyed[int]("state")
	var wg sync.WaitGroup
	values := make([]int, 50)
	for range 10 {
		wg.Go(func() {
			for key := range values {
				_, release, err := keys.Lock(context.Background(), key, 5*time.Second)
				if err != nil {
					t.Error(err)
					return
				}
				values[key]++
				release()
			}
		})
	}
	wg.Wait()
	for _, value := range values {
		if value != 10 {
			t.Fatal(values)
		}
	}
	if len(keys.entries) != 0 {
		t.Fatal("leaf entries leaked")
	}
}

func TestDeviceControlSuborder(t *testing.T) {
	work := NewRWMutex(LevelDevice, "substore-work")
	ctx, release, err := work.RLock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, releaseControl := lockMutex(t, NewMutex(LevelDeviceControl, "substore-control"), ctx)
	defer releaseControl()
	_, releaseRepo := lockMutex(t, NewMutex(LevelRepository, "repository"), ctx)
	releaseRepo()
}

func TestDeviceControlReverseOrderProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LOCKX_ORDER_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDeviceControlReverseOrderProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LOCKX_ORDER_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("order child: %v\n%s", err, out)
		}
		return
	}
	ctx, releaseControl := lockMutex(t, NewMutex(LevelDeviceControl, "control"), context.Background())
	defer releaseControl()
	_, releaseWork, err := NewRWMutex(LevelDevice, "work").RLock(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	releaseWork()
	ctx, releaseRepo := lockMutex(t, NewMutex(LevelRepository, "repo"), ctx)
	defer releaseRepo()
	ctx, releaseLeaf, err := NewLeafKeyed[string]("leaf").Lock(ctx, "state", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseLeaf()
	_, releaseDevice := lockMutex(t, NewMutex(LevelDevice, "after-leaf"), ctx)
	releaseDevice()
	if Violations() != 2 {
		t.Fatal("reverse suborder or parent level hidden by leaf", Violations())
	}
}
