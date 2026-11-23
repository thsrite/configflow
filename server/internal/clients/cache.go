package clients

import (
	"configflow/internal/routers/spec"
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/routerdoc"
)

// RouterReading carries one ordered router result; failures never hide peers.
type RouterReading struct {
	Reading *routerdoc.Reading
	Error   string
}

// Snapshot caches router evidence only. Saved identities are joined on each call.
type Snapshot struct {
	Readings  []RouterReading
	CheckedAt string
}

// CacheMetadata reports whether evidence was reused and the last refresh failed.
type CacheMetadata struct {
	Cached       bool   `json:"cached"`
	Stale        bool   `json:"stale"`
	RefreshError string `json:"refresh_error"`
}
type cacheEntry struct {
	signature [32]byte
	snapshot  *Snapshot
	attempted time.Time
	error     string
}
type cachedSnapshot struct {
	snapshot Snapshot
	meta     CacheMetadata
}

// DiscoveryCache is an app-owned L5 memory cache (60-second stale marker, 128
// profiles). There is no I/O under mu; Runtime tracks coalesced loads.
type DiscoveryCache struct {
	mu      sync.Mutex
	entries map[string]*cacheEntry
	order   []string
	flight  *lockx.Flight[*cacheEntry, cachedSnapshot]
	clock   clock.Clock
}

// NewDiscoveryCache creates an isolated cache for one application.
func NewDiscoveryCache(spawner lockx.Spawner, clockSource clock.Clock) *DiscoveryCache {
	if spawner == nil {
		spawner = lifecycle.DefaultSpawner{}
	}
	return &DiscoveryCache{entries: map[string]*cacheEntry{}, clock: clockSource, flight: lockx.NewFlight[*cacheEntry, cachedSnapshot](spawner, "client-discovery-cache")}
}

// Invalidate removes an entry without allowing an in-flight old load to revive it.
func (c *DiscoveryCache) Invalidate(profileID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, profileID)
	c.removeOrder(profileID)
}
func (c *DiscoveryCache) removeOrder(id string) {
	for i, v := range c.order {
		if v == id {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}
func cloneSnapshot(s Snapshot) Snapshot {
	out := Snapshot{CheckedAt: s.CheckedAt, Readings: make([]RouterReading, len(s.Readings))}
	for i, item := range s.Readings {
		out.Readings[i].Error = item.Error
		if item.Reading == nil {
			continue
		}
		r := routerdoc.NewReading()
		r.Complete = item.Reading.Complete
		r.Notes = append([]string{}, item.Reading.Notes...)
		r.LAN = append(r.LAN, item.Reading.LAN...)
		for ip, v := range item.Reading.Own {
			r.Own[ip] = v
		}
		for ip, s := range item.Reading.Sightings {
			v := *s
			v.Sources = append([]string{}, s.Sources...)
			v.FixedIP = append(json.RawMessage(nil), s.FixedIP...)
			if s.LastSeen != nil {
				n := *s.LastSeen
				v.LastSeen = &n
			}
			r.Sightings[ip] = &v
		}
		out.Readings[i].Reading = r
	}
	return out
}
func cachedResult(e *cacheEntry, cached bool, now time.Time) cachedSnapshot {
	return cachedSnapshot{cloneSnapshot(*e.snapshot), CacheMetadata{cached, now.Sub(e.attempted) >= 60*time.Second, e.error}}
}
func useful(s *Snapshot) bool {
	if s == nil {
		return false
	}
	for _, v := range s.Readings {
		if v.Reading != nil {
			return true
		}
	}
	return false
}

// Read reuses an existing snapshot until explicitly refreshed, even once stale.
// Callers receive deep copies; credentials exist only in a digest in the cache.
func (c *DiscoveryCache) Read(ctx context.Context, profileID string, rs []*spec.Router, load func(context.Context) (Snapshot, error), refresh bool) (Snapshot, CacheMetadata, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return Snapshot{}, CacheMetadata{}, err
	}
	records := make([]json.RawMessage, 0, len(rs))
	for _, r := range rs {
		record, err := r.StoredRecord()
		if err != nil {
			return Snapshot{}, CacheMetadata{}, err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(record, &fields); err != nil {
			return Snapshot{}, CacheMetadata{}, err
		}
		canonical, err := json.Marshal(fields)
		if err != nil {
			return Snapshot{}, CacheMetadata{}, err
		}
		records = append(records, canonical)
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return Snapshot{}, CacheMetadata{}, err
	}
	signature := sha256.Sum256(raw)
	clock := c.clock
	if clock == nil {
		clock = lifecycle.ClockFrom(ctx)
	}
	c.mu.Lock()
	if err := ctx.Err(); err != nil {
		c.mu.Unlock()
		return Snapshot{}, CacheMetadata{}, err
	}
	e := c.entries[profileID]
	if e == nil || e.signature != signature {
		e = &cacheEntry{signature: signature}
		c.entries[profileID] = e
	}
	c.removeOrder(profileID)
	c.order = append(c.order, profileID)
	if len(c.order) > 128 {
		delete(c.entries, c.order[0])
		c.order = c.order[1:]
	}
	if e.snapshot != nil && !refresh {
		result := cachedResult(e, true, clock.Now())
		c.mu.Unlock()
		return result.snapshot, result.meta, nil
	}
	c.mu.Unlock()
	result, err, _ := c.flight.Do(ctx, e, func(work context.Context) (cachedSnapshot, error) {
		// A cold reader may have reached Flight just after the first owner finished.
		c.mu.Lock()
		if e.snapshot != nil && !refresh {
			v := cachedResult(e, true, clock.Now())
			c.mu.Unlock()
			return v, nil
		}
		c.mu.Unlock()
		snapshot, err := load(work)
		if err != nil {
			return cachedSnapshot{}, err
		}
		failure := ""
		if !useful(&snapshot) {
			messages := []string{}
			for _, r := range snapshot.Readings {
				if r.Error != "" {
					messages = append(messages, r.Error)
				}
			}
			failure = strings.Join(messages, "；")
			if failure == "" {
				failure = messageCacheFailed
			}
		}
		if err = work.Err(); err != nil {
			return cachedSnapshot{}, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := work.Err(); err != nil {
			return cachedSnapshot{}, err
		}
		if failure == "" || !useful(e.snapshot) {
			copy := cloneSnapshot(snapshot)
			e.snapshot = &copy
			failure = ""
		}
		e.error = failure
		e.attempted = clock.Now()
		return cachedResult(e, failure != "", clock.Now()), nil
	})
	if err != nil {
		return Snapshot{}, CacheMetadata{}, err
	}
	return cloneSnapshot(result.snapshot), result.meta, nil
}
