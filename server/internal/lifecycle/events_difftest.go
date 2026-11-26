//go:build configflow_difftest

package lifecycle

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

var eventsMu sync.Mutex

func emitEvent(at time.Time, name, event string) {
	path := os.Getenv("CONFIGFLOW_LIFECYCLE_EVENTS")
	if path == "" {
		return
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		logger.Errorf(msgEventsFailed, err)
		return
	}
	defer f.Close()
	if err := json.NewEncoder(f).Encode(struct {
		Time  time.Time `json:"t"`
		Loop  string    `json:"loop"`
		Event string    `json:"event"`
	}{at, name, event}); err != nil {
		logger.Errorf(msgEventsFailed, err)
	}
}
