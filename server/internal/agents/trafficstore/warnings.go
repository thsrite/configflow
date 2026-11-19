package trafficstore

import (
	"configflow/internal/logx"
	"configflow/internal/redact"
	"crypto/sha256"
	"sync"
	"time"
)

var corruptionWarnings = struct {
	sync.Mutex
	last map[[32]byte]time.Time
}{last: map[[32]byte]time.Time{}}

func warnStore(path, field string) {
	key := sha256.Sum256([]byte(path + "\x00" + field))
	at := time.Now()
	corruptionWarnings.Lock()
	defer corruptionWarnings.Unlock()
	if last, ok := corruptionWarnings.last[key]; ok && at.Sub(last) < 5*time.Minute {
		return
	}
	if len(corruptionWarnings.last) >= 4096 {
		for key, last := range corruptionWarnings.last {
			if at.Sub(last) >= 5*time.Minute {
				delete(corruptionWarnings.last, key)
			}
		}
		if len(corruptionWarnings.last) >= 4096 {
			return
		}
	}
	corruptionWarnings.last[key] = at
	logx.Get("backend.agents.traffic_store").Warningf("%s", redact.EventText(msgCorruptStore, nil))
}
