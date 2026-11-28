package check

import (
	"sync"
	"time"

	"configflow/internal/jsonstore"
)

type probeStore[T any] struct {
	files  *jsonstore.Store
	cached []*probeSnapshot[T] // most recently used last, guarded by probeLock
}

type probeSnapshot[T any] struct {
	path        string
	signature   jsonstore.Signature
	file        probeFile
	retained    []retainedHost
	projectOnce sync.Once
	projected   []projectedProbe[T]
}

type projectedProbe[T any] struct {
	probe T
	err   error
}

// ProjectedProbeStore caches a caller's typed view alongside each file version.
type ProjectedProbeStore[T any] struct {
	*probeStore[T]
	project func(host, checkedAt string, record TimedRecord) (T, error)
}

// WithProbeProjection opens a projected store for the same per-Agent files.
func WithProbeProjection[T any](s *ProbeStore, project func(host, checkedAt string, record TimedRecord) (T, error)) *ProjectedProbeStore[T] {
	return &ProjectedProbeStore[T]{&probeStore[T]{files: s.files}, project}
}

// LatestProbes selects the newest retained record for each host. The nested
// values belong to immutable snapshots and must be copied before mutation.
func (s *ProjectedProbeStore[T]) LatestProbes(agentIDs []string) (map[string]T, error) {
	out := map[string]T{}
	stamps := map[string]string{}
	invalid := map[string]error{}
	var order []string
	for _, id := range agentIDs {
		bound := oldest(time.Now(), ProbeRetentionDays)
		snap := s.loadSnapshot(id)
		snap.projectOnce.Do(func() {
			snap.projected = make([]projectedProbe[T], len(snap.retained))
			for i, h := range snap.retained {
				p, err := s.project(h.host, h.checkedAt, h.entry)
				snap.projected[i] = projectedProbe[T]{p, err}
			}
		})
		if len(out) == 0 {
			out = make(map[string]T, len(snap.retained))
			stamps = make(map[string]string, len(snap.retained))
		}
		for i, h := range snap.retained {
			if h.checkedAt < bound {
				continue
			}
			if prior, ok := stamps[h.host]; ok {
				if prior >= h.checkedAt {
					continue
				}
			} else {
				order = append(order, h.host)
			}
			stamps[h.host] = h.checkedAt
			v := snap.projected[i]
			out[h.host] = v.probe
			if v.err != nil {
				invalid[h.host] = v.err
			} else {
				delete(invalid, h.host)
			}
		}
	}
	if len(invalid) != 0 {
		for _, host := range order {
			if err := invalid[host]; err != nil {
				return out, err
			}
		}
	}
	return out, nil
}
