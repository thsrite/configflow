package routers

import (
	"configflow/internal/routerdoc"
	"context"
	"encoding/json"
	"time"
)

// ClientReadOptions bounds discovery and prioritizes saved IPv4 hosts for probes.
// Callers set Deadline before fan-out; each driver enforces it on every I/O.
type ClientReadOptions struct {
	Priority map[string]bool
	Deadline time.Time
}

// ClientReader reads LAN evidence without changing device configuration.
// Ping probes are the only active operation. Drivers belong to one read owner.
type ClientReader interface {
	ReadClients(context.Context, ClientReadOptions) (*routerdoc.Reading, error)
	Close()
}

// FromConfig returns owned router records, ignoring malformed container/items
// as the existing inspection service does. Unknown record fields are retained.
func FromConfig(config json.RawMessage) []*Router { return routersOf(config) }

// StoredRecord returns retained stored fields (including malformed legacy
// members) when settings are unchanged; otherwise it encodes the edited record.
func (r *Router) StoredRecord() (json.RawMessage, error) { return releaseRouterRecord(r) }
