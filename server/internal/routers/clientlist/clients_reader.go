package clientlist

import (
	"context"
	"time"

	"configflow/internal/routerdoc"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
)

// ReadOptions bounds discovery and prioritizes saved IPv4 hosts for probes.
// Callers set Deadline before fan-out; each driver enforces it on every I/O.
type ReadOptions struct {
	Priority map[string]bool
	Deadline time.Time
}

// Reader reads LAN evidence without changing device configuration.
// Ping probes are the only active operation. Drivers belong to one read owner.
type Reader interface {
	ReadClients(context.Context, ReadOptions) (*routerdoc.Reading, error)
	Close()
}

type rosDevice struct{ *ros.Client }

func (d rosDevice) ROSClient() *ros.Client { return d.Client }

type ikuaiDevice struct{ *ikuai.Client }

func (d ikuaiDevice) IkuaiClient() *ikuai.Client { return d.Client }

// NewROS wraps a RouterOS client as a LAN client reader.
func NewROS(c *ros.Client) Reader { return rosDevice{c} }

// NewIkuai wraps an iKuai client as a LAN client reader.
func NewIkuai(c *ikuai.Client) Reader { return ikuaiDevice{c} }

// Open constructs a LAN client reader for the configured router.
func Open(r *spec.Router) Reader {
	if r == nil {
		return nil
	}
	switch r.Type {
	case "routeros":
		return NewROS(ros.NewClient(r.Endpoint, r.Username, r.Password, r.VerifyTLS))
	case "ikuai":
		return NewIkuai(ikuai.NewClient(r.Endpoint, r.Username, r.Password, r.VerifyTLS))
	default:
		return nil
	}
}
