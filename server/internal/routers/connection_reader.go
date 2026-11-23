package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"net/url"
	"strconv"
	"time"
)

// ConnectionQuery restricts evidence reads to TCP tuples and necessary counters.
// LegacySource supports older RouterOS combined source address/port columns.
type ConnectionQuery struct {
	IPv6            bool
	DestinationPort int
	SourceAddress   string
	SourcePort      int
	LegacySource    bool
}

// ReadConnections reads only firewall connection evidence, isolating bad rows.
// A false complete flag must never be interpreted as an empty baseline. Results
// are bounded; caller-owned evidence never exposes unrelated connection rows.
func (d *RouterOS) ReadConnections(ctx context.Context, q ConnectionQuery, timeout time.Duration) ([]Row, bool, error) {
	if q.DestinationPort < 0 || q.DestinationPort > 65535 || q.SourcePort < 0 || q.SourcePort > 65535 {
		return nil, false, device.Invalid(device.MessageEditFormat)
	}
	path := "/ip/firewall/connection"
	if q.IPv6 {
		path = "/ipv6/firewall/connection"
	}
	values := url.Values{"protocol": {"tcp"}, ".proplist": {".id,protocol,src-address,src-port,dst-address,dst-port,orig-packets,orig-fasttrack-packets"}}
	if q.DestinationPort > 0 {
		values.Set("dst-port", strconv.Itoa(q.DestinationPort))
	}
	if q.SourceAddress != "" {
		address := q.SourceAddress
		if q.LegacySource {
			if q.IPv6 {
				address = "[" + address + "]"
			}
			address += ":" + strconv.Itoa(q.SourcePort)
		} else if q.SourcePort > 0 {
			values.Set("src-port", strconv.Itoa(q.SourcePort))
		}
		values.Set("src-address", address)
	}
	if timeout <= 0 {
		timeout = ros.Timeout
	}
	rows, complete, err := d.clientList(ctx, path+"?"+values.Encode(), timeout)
	if len(rows) > 256 {
		rows = rows[:256]
		complete = false
	}
	return rows, complete, err
}
