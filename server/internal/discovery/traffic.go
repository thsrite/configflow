package discovery

import (
	"container/list"
	"context"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"sync"
	"unsafe"

	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsondecode"
)

// Traffic files contain one atomic revision per Agent, including status and all
// retained days. Only this immutable input is shared; all listing and job state
// is derived from the current profile, rules, probes and date on every call.
type traffic struct {
	Status       collectionStatus        `json:"status"`
	Days         trafficRows[trafficDay] `json:"days"`
	storeState   string
	storeMessage string
}

type trafficDay struct {
	IPOnly int64                    `json:"ip_only_conns"`
	Hosts  trafficRows[trafficHost] `json:"hosts"`
}

type trafficHost struct {
	First  string             `json:"first_seen"`
	Last   string             `json:"last_seen"`
	Ports  trafficRows[int64] `json:"ports"`
	Routes trafficRows[Stats] `json:"by_route"`
}

type trafficRow[V any] struct {
	key   string
	value V
}

// trafficRows is the ordered, read-only projection of a validated store object.
// Ranking ties depend on file order. Decode each key and value just once.
type trafficRows[V any] []trafficRow[V]

// UnmarshalJSONFrom reads validated rows in storage order.
func (r *trafficRows[V]) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	open, err := d.ReadToken()
	if err != nil {
		return err
	}
	if open.Kind() != '{' {
		return fmt.Errorf("expected traffic object, got %s", open.Kind())
	}
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil {
			return err
		}
		row := trafficRow[V]{key: key.String()}
		if err := jsonv2.UnmarshalDecode(d, &row.value); err != nil {
			return err
		}
		*r = append(*r, row)
	}
	_, err = d.ReadToken()
	return err
}

type roundTrafficKey struct{}
type roundTraffic map[string]trafficSnapshot
type trafficSnapshot struct {
	report   traffic
	revision trafficstore.DocumentRevision
}

const (
	trafficCacheBytes   = 16 << 20
	trafficCacheEntries = 32
)

// rowBytes charges the backing array (including unused capacity) and key text.
func rowBytes[V any](rows trafficRows[V]) int {
	size := cap(rows) * int(unsafe.Sizeof(trafficRow[V]{}))
	for _, r := range rows {
		size += len(r.key)
	}
	return size
}

func (t *traffic) retainedBytes(path string) int {
	// Include fixed bookkeeping, both path strings and per-map storage. The
	// Go allocator's exact size classes depend on the platform.
	size := 512 + 2*len(path) + len(t.Status.Value) + len(t.Status.MihomoVersion) + rowBytes(t.Days)
	if t.Status.At != nil {
		size += len(*t.Status.At)
	}
	for _, day := range t.Days {
		size += rowBytes(day.value.Hosts)
		for _, item := range day.value.Hosts {
			h := item.value
			size += len(h.First) + len(h.Last) + rowBytes(h.Ports) + rowBytes(h.Routes)
			for _, route := range h.Routes {
				if kinds := route.value.FailKinds; kinds != nil {
					size += 256 + 64*len(kinds)
					for k := range kinds {
						size += len(k)
					}
				}
			}
		}
	}
	return size
}

type trafficCacheEntry struct {
	path string
	size int
	trafficSnapshot
}

type trafficCache struct {
	mu    sync.Mutex
	order list.List // most recently read first; at most trafficCacheEntries
	bytes int
}

func (c *trafficCache) remove(e *list.Element) {
	c.bytes -= c.order.Remove(e).(*trafficCacheEntry).size
}

func (c *trafficCache) get(path string) (trafficSnapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for e := c.order.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*trafficCacheEntry)
		if entry.path == path {
			if !entry.revision.Current() {
				c.remove(e)
				break
			}
			c.order.MoveToFront(e)
			return entry.trafficSnapshot, true
		}
	}
	return trafficSnapshot{}, false
}

func (c *trafficCache) put(path string, snap trafficSnapshot) {
	size := snap.report.retainedBytes(path)
	if size > trafficCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A write or rollover can race the unlocked decode.
	if !snap.revision.Current() {
		return
	}
	for e := c.order.Front(); e != nil; e = e.Next() {
		if e.Value.(*trafficCacheEntry).path == path {
			c.remove(e)
			break
		}
	}
	for c.order.Len() >= trafficCacheEntries || c.bytes+size > trafficCacheBytes {
		c.remove(c.order.Back())
	}
	c.order.PushFront(&trafficCacheEntry{path, size, snap})
	c.bytes += size
}

func (s *Service) loadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
	if err := ctx.Err(); err != nil {
		return traffic{}, err
	}
	store, err := s.trafficStore()
	if err != nil {
		return traffic{}, err
	}
	if !full {
		snap := store.InspectStatus(a.ID)
		return trafficWithStatus(snap)
	}
	path := store.Path(a.ID)
	round, _ := ctx.Value(roundTrafficKey{}).(roundTraffic)
	if snap, ok := round[path]; ok && snap.revision.Current() {
		return snap.report, nil
	}
	snap, ok := s.trafficCache.get(path)
	if !ok {
		doc := store.LoadDocument(a.ID, nil)
		report, err := trafficWithStatus(trafficstore.StatusSnapshot{Status: doc.StatusJSON(), State: doc.StoreState()})
		if err != nil {
			return traffic{}, err
		}
		for date, raw := range doc.DayRecords() {
			var day trafficDay
			if err := jsondecode.Unmarshal(raw, &day); err != nil {
				return traffic{}, fmt.Errorf("decode traffic day: %w", err)
			}
			report.Days = append(report.Days, trafficRow[trafficDay]{date, day})
		}
		snap = trafficSnapshot{report, doc.Revision()}
		s.trafficCache.put(path, snap)
	}
	if round != nil {
		// Also retain oversized reports for the lifetime of this synchronous
		// round, as before. They never enter the shared cache.
		round[path] = snap
	}
	return snap.report, nil
}

func trafficWithStatus(snap trafficstore.StatusSnapshot) (traffic, error) {
	report := traffic{storeState: string(snap.State)}
	if snap.State == trafficstore.StoreStateDamaged {
		report.storeMessage = MsgStoreDamaged
	}
	if len(snap.Status) > 0 {
		if err := jsondecode.Unmarshal(snap.Status, &report.Status); err != nil {
			return traffic{}, fmt.Errorf("decode traffic status: %w", err)
		}
	}
	return report, nil
}
