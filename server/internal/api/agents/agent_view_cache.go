package agents

// Derived Agent view cache (O10-R4).
//
// The direct computation (publicAgentView, content sources from the borrowed
// Config) still encodes each record's members and histories for every read.
// It stays the definition and the path taken whenever the cache is off; the
// cache only adds reuse. An entry is derived only from:
//
//   - the stored registry generation (store head) and the record's position
//     and id in it; the credentials masked are the generation's stored Agent
//     credentials plus those in the live members,
//   - the members a read lays over the stored record (heartbeat runtime fields,
//     last_heartbeat and status, including the offline decision made at read
//     time), fingerprinted on every read,
//   - AGENT_REGISTRATION_KEY, which credential collection always adds.
//
// A content source is derived from the head and the resolved profile. Only one
// generation is kept, with at most one entry per Agent and view. Content sync
// status, update flags and the response writer's token pass run per request.
// Reads inside a write transaction (unpublished records) are never cached.
//
// CONFIGFLOW_AGENT_VIEW_CACHE=0 turns the cache off (direct computation).

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"slices"
	"sync"
	"sync/atomic"

	core "configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
)

// AgentViewCacheEnv names the switch; "0" disables the derived view cache.
const AgentViewCacheEnv = "CONFIGFLOW_AGENT_VIEW_CACHE"

type viewMode uint8

const (
	viewFull    viewMode = iota // credentials from the live record, histories included
	viewSummary                 // history-free record
)

type viewKey struct {
	index int
	id    string
	mode  viewMode
}

type viewEntry struct {
	live   [sha256.Size]byte
	public map[string]json.RawMessage
}

type agentViews struct {
	enabled atomic.Bool
	mu      sync.Mutex
	// generation identity of entries (full Head: includes Agent state)
	repository *repo.Repository
	head       store.Head
	env        string
	entries    map[viewKey]viewEntry
	// generation identity of sources (configuration-only SameConfig: content
	// sources come from repo.Config and read no Agent state)
	sourceRepo *repo.Repository
	sourceHead store.Head
	sources    map[string]contentSnapshot
}

var views = newAgentViews(os.Getenv(AgentViewCacheEnv) != "0")

func newAgentViews(enabled bool) *agentViews {
	v := &agentViews{}
	v.enabled.Store(enabled)
	return v
}

// setAgentViewCache switches the cache and returns a function restoring the
// previous state (tests and measurements).
func setAgentViewCache(on bool) (restore func()) {
	previous := views.enabled.Swap(on)
	views.reset()
	return func() { views.enabled.Store(previous); views.reset() }
}

func (v *agentViews) reset() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.repository, v.head, v.env = nil, store.Head{}, ""
	v.sourceRepo, v.sourceHead = nil, store.Head{}
	v.entries, v.sources = nil, nil
}

// generation selects the public view map of (repository, head) under v.mu,
// starting a new generation when it differs. It reports false when nothing
// may be cached.
func (v *agentViews) generation(repository *repo.Repository, gen core.Generation) bool {
	if !v.enabled.Load() || !gen.Shared || repository == nil {
		return false
	}
	env := os.Getenv("AGENT_REGISTRATION_KEY")
	if v.repository != repository || v.head != gen.Head || v.env != env || v.entries == nil {
		v.repository, v.head, v.env = repository, gen.Head, env
		v.entries = map[viewKey]viewEntry{}
	}
	return true
}

// sourceGeneration selects the content-source map of (repository, head) under
// v.mu using SameConfig so state-only commits do not invalidate resolved
// profile content sources.
func (v *agentViews) sourceGeneration(repository *repo.Repository, gen core.Generation) bool {
	if !v.enabled.Load() || !gen.Shared || repository == nil {
		return false
	}
	if v.sourceRepo != repository || !v.sourceHead.SameConfig(gen.Head) || v.sources == nil {
		v.sourceRepo, v.sourceHead = repository, gen.Head
		v.sources = map[string]contentSnapshot{}
	}
	return true
}

// liveMembers are the members a read may change on a stored record.
var liveMembers = append(slices.Clone(core.RuntimeFields), "last_heartbeat", "status")

func liveFingerprint(record *model.Agent) ([sha256.Size]byte, error) {
	h := sha256.New()
	var size [8]byte
	for _, name := range liveMembers {
		raw, present, err := model.EncodeMember(record, name)
		if err != nil {
			return [sha256.Size]byte{}, err
		}
		h.Write([]byte(name))
		if !present {
			h.Write([]byte{0})
			continue
		}
		binary.BigEndian.PutUint64(size[:], uint64(len(raw)))
		h.Write([]byte{1})
		h.Write(size[:])
		h.Write(raw)
	}
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// public returns compute()'s projection of live, from the cache when the same
// generation, Agent, view and live members were projected before. The result
// is the caller's own map; its values must not be modified in place.
func (v *agentViews) public(repository *repo.Repository, gen core.Generation, key viewKey, live *model.Agent,
	compute func() (map[string]json.RawMessage, error)) (map[string]json.RawMessage, error) {
	v.mu.Lock()
	cacheable := v.generation(repository, gen)
	v.mu.Unlock()
	if !cacheable {
		return compute()
	}
	fingerprint, err := liveFingerprint(live)
	if err != nil {
		return compute()
	}
	v.mu.Lock()
	if v.generation(repository, gen) {
		if entry, ok := v.entries[key]; ok && entry.live == fingerprint {
			v.mu.Unlock()
			return maps.Clone(entry.public), nil
		}
	}
	v.mu.Unlock()
	public, err := compute()
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	if v.generation(repository, gen) {
		v.entries[key] = viewEntry{fingerprint, maps.Clone(public)}
	}
	v.mu.Unlock()
	return public, nil
}

// source returns the content source of profile for the generation. Only
// successes and repository errors (a missing profile) are remembered.
func (v *agentViews) source(repository *repo.Repository, gen core.Generation, profile string,
	compute func() contentSnapshot) contentSnapshot {
	v.mu.Lock()
	if v.sourceGeneration(repository, gen) {
		if saved, ok := v.sources[profile]; ok {
			v.mu.Unlock()
			return saved
		}
	}
	v.mu.Unlock()
	snapshot := compute()
	var repositoryError *repo.Error
	if snapshot.err != nil && !errors.As(snapshot.err, &repositoryError) {
		return snapshot
	}
	v.mu.Lock()
	if v.sourceGeneration(repository, gen) {
		v.sources[profile] = snapshot
	}
	v.mu.Unlock()
	return snapshot
}
