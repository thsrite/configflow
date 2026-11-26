package metrics

import (
	"configflow/internal/redact"
	"crypto/sha256"
	"sync"
	"time"
)

type warningKey struct {
	object [32]byte
	field  string
}
type warningLimiter struct {
	mu   sync.Mutex
	last map[warningKey]time.Time
}

func (l *warningLimiter) allow(object, field string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	key := warningKey{sha256.Sum256([]byte(object)), field}
	at := time.Now()
	if previous, ok := l.last[key]; ok && at.Sub(previous) < 5*time.Minute {
		return false
	}
	if len(l.last) >= 4096 {
		for key, previous := range l.last {
			if at.Sub(previous) >= 5*time.Minute {
				delete(l.last, key)
			}
		}
		if len(l.last) >= 4096 {
			return false
		}
	}
	if l.last == nil {
		l.last = map[warningKey]time.Time{}
	}
	l.last[key] = at
	return true
}
func (h *History) warnCorrupt(id, field string) {
	if h.warnings.allow(id, field) {
		historyLogger.Warningf("%s", redact.EventText(msgCorruptMetric+field, nil))
	}
}

func (h *TrafficHistory) warnCorrupt(id, field string) {
	if h.warnings.allow(id, field) {
		trafficLogger.Warningf("%s", redact.EventText(msgCorruptMetric+field, nil))
	}
}
