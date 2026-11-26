package routers

import (
	"configflow/internal/redact"
	"configflow/internal/routers/spec"
	"encoding/json"
)

func managementLogText(config json.RawMessage, seeds redact.Secrets, r *spec.Router, value string) string {
	return eventText(managementSafeText(config, seeds, r, value))
}

// Clear invalidates cached and in-flight generations without abandoning their
// tracked work. Late completion cannot repopulate or overwrite newer entries.
func (c *PendingCache) Clear() { c.mu.Lock(); defer c.mu.Unlock(); clear(c.entries); c.order.Init() }
