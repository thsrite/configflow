//go:build configflow_difftest

package faultprobe

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"configflow/internal/clock"
	"configflow/internal/logx"
)

var probeState = struct {
	sync.Mutex
	active map[Phase]int
}{active: map[Phase]int{}}
var eventMu sync.Mutex
var probeClock clock.Clock = clock.Real{}
var logger = logx.Get("test.faultprobe")

// Token reads the harness token only from a private real directory/file.
func Token() ([]byte, error) {
	dir := os.Getenv(ControlEnv)
	if dir == "" {
		return nil, errors.New("fault control directory is not configured")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return nil, errors.New("fault control directory must have mode 0700")
	}
	path := filepath.Join(dir, "token")
	info, err = os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return nil, errors.New("fault token must be a regular mode-0600 file")
	}
	token, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(token) < 32 || len(token) > 256 {
		return nil, errors.New("fault token must contain 32 to 256 bytes")
	}
	return token, nil
}

func event(dir string, phase Phase, kind string) {
	eventMu.Lock()
	defer eventMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		logger.Warningf("fault probe event unavailable: %s", phase)
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(struct {
		Phase Phase     `json:"phase"`
		Event string    `json:"event"`
		Time  time.Time `json:"time"`
	}{phase, kind, time.Now().UTC()})
}

// Point observes a registered phase. A matching pause file delays it until
// removed, ctx ends, or MaxWait expires. It never changes the work's result.
func Point(ctx context.Context, phase Phase) {
	if !registered(phase) {
		return
	}
	dir := os.Getenv(ControlEnv)
	if dir == "" {
		return
	}
	if _, err := Token(); err != nil {
		return
	}
	probeState.Lock()
	probeState.active[phase]++
	probeState.Unlock()
	defer func() { probeState.Lock(); probeState.active[phase]--; probeState.Unlock() }()
	event(dir, phase, "entered")
	waiting, cancel := context.WithTimeout(ctx, MaxWait)
	defer cancel()
	for {
		if _, err := os.Stat(filepath.Join(dir, "pause."+string(phase))); err != nil {
			event(dir, phase, "released")
			return
		}
		timer := probeClock.NewTimer(10 * time.Millisecond)
		select {
		case <-waiting.Done():
			timer.Stop()
			kind := "cancelled"
			if ctx.Err() == nil {
				kind = "timeout"
				logger.Warningf("fault barrier timed out: %s", phase)
			}
			event(dir, phase, kind)
			return
		case <-timer.C():
			timer.Stop()
		}
	}
}

// Active returns only registered phase names, never configuration or tokens.
func Active() []Phase {
	probeState.Lock()
	defer probeState.Unlock()
	out := []Phase{}
	for name, n := range probeState.active {
		if n > 0 {
			out = append(out, name)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
