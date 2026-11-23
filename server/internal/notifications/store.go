package notifications

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

// The persistent attention items of backend/utils/notification_store.py:
// DATA_DIR/notifications.json is guarded by a process-wide leaf mutex. The
// instance lock excludes other servers; legacy lock files are left untouched.

var guard = lockx.NewLeafKeyed[string]("notification-store")

// Observation tickets order collection starts against committed mutations.
// appliedVersions is protected by guard; unrelated data directories do not
// invalidate each other's observations.
var observationSequence atomic.Uint64
var appliedVersions = make(map[string]uint64)

// Clock is the time source of the store (tests replace it).
var Clock = time.Now

// Now is notification_store.now(): UTC ISO time with microseconds.
func Now() string {
	return Clock().UTC().Format("2006-01-02T15:04:05.000000") + "+00:00"
}

// ErrInvalidStore is the ValueError of a store file that is not
// {"items": {...}}.
var ErrInvalidStore = errors.New("Invalid notification store")

var errInvalidItem = errors.New(msgItemShape)

// IncidentFingerprint identifies an incident using native JSON (D2 policy B).
func IncidentFingerprint(key string, item *Item) (string, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return "", err
	}
	var members map[string]json.RawMessage
	if err = json.Unmarshal(raw, &members); err != nil {
		return "", err
	}
	return fingerprintFromMembers(key, item.Source, members)
}

func fingerprintFromMembers(key, source string, members map[string]json.RawMessage) (string, error) {
	fields := make(map[string]json.RawMessage, 6)
	keys := []string{"title", "message", "severity", "status_label", "target"}
	if source == "router-check" && strings.HasSuffix(key, ":changes") {
		keys = []string{"signature"}
	} else if source == "router-check" {
		keys = append(keys, "signature")
	} else {
		keys = append(keys, "evidence")
	}
	for _, field := range keys {
		fields[field] = members[field]
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	data, err = jsonresp.Canonical(data)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func dismissedItem(key string, item *Item) (*Item, error) {
	identity, err := IncidentFingerprint(key, item)
	if err != nil {
		return nil, err
	}
	return &Item{State: "dismissed", Fingerprint: &identity, Source: item.Source}, nil
}

// UpdateItems owns a shutdown-aware store transaction independently of a client disconnect.
func UpdateItems(ctx context.Context, dataDir string, fn func(*Items) error) error {
	return transaction(lifecycle.Detach(ctx), dataDir, 0, true, func(items *Items, _ bool) error { return fn(items) })
}

func transaction(ctx context.Context, dataDir string, ticket uint64, persist bool, fn func(items *Items, apply bool) error) error {
	key, err := repo.ResolveDataDir(dataDir)
	if err != nil {
		return err
	}
	path := filepath.Join(dataDir, "notifications.json")
	_, release, err := guard.Lock(ctx, "notifications", 5*time.Second)
	if err != nil {
		return err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return err
	}
	if persist {
		if err := os.MkdirAll(dataDir, 0o777); err != nil {
			return err
		}
	}

	var data storeDocument
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data.Items = newItems()
	case err != nil:
		return err
	default:
		if data, err = decodeStore(raw); err != nil {
			return err
		}
	}
	var before []byte
	if persist {
		before, err = json.Marshal(data)
		if err != nil {
			return err
		}
	}
	migrated := newItems()
	for key, item := range data.Items.All() {
		if item == nil {
			return ErrInvalidStore
		}
		if item.State != "pending" && item.State != "dismissed" {
			continue
		}
		if item.Read != nil && *item.Read {
			value, err := dismissedItem(key, item)
			if err != nil {
				return err
			}
			migrated.Set(key, value)
		} else {
			migrated.Set(key, item)
		}
	}
	data.Items = migrated
	apply := ticket == 0 || ticket >= appliedVersions[key]
	if err := fn(migrated, apply); err != nil {
		return err
	}
	if !persist {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if bytes.Equal(encoded, before) {
		if ticket != 0 && apply {
			appliedVersions[key] = ticket
		}
		return nil
	}
	temporary, err := os.CreateTemp(dataDir, ".notifications-")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if ticket != 0 && apply {
		appliedVersions[key] = ticket
	} else {
		appliedVersions[key] = observationSequence.Add(1)
	}
	return nil
}

// ObserveItem applies a typed incident observation. It never mutates its input.
func ObserveItem(items *Items, key string, observation *Item) error {
	if observation == nil {
		return ErrInvalidStore
	}
	switch observation.State {
	case "unknown":
		return nil
	case "resolved", "inactive":
		items.Delete(key)
		return nil
	}
	previous, _ := items.Get(key)
	// JSON member overlays retain the old receipt's unowned fields. State and
	// evidence are managed by the store, not copied from an observation.
	raw, err := json.Marshal(observation)
	if err != nil {
		return err
	}
	var patch map[string]json.RawMessage
	if err = json.Unmarshal(raw, &patch); err != nil {
		return err
	}
	identity, err := fingerprintFromMembers(key, observation.Source, patch)
	if err != nil {
		return err
	}
	if previous != nil && previous.State == "dismissed" && stringValue(previous.Fingerprint) == identity {
		return nil
	}
	stamp := Now()
	evidence := observation.Evidence
	if len(evidence) == 0 {
		evidence = jsonString("")
	}
	seen := stamp
	if len(observation.CheckedAt) > 0 && string(observation.CheckedAt) != "null" && string(observation.CheckedAt) != `""` {
		var value string
		if json.Unmarshal(observation.CheckedAt, &value) == nil {
			seen = value
		}
	}
	delete(patch, "state")
	delete(patch, "evidence")
	result := &Item{}
	if previous != nil && previous.State == "pending" {
		baseRaw, err := json.Marshal(previous)
		if err != nil {
			return err
		}
		var fields map[string]json.RawMessage
		if err = json.Unmarshal(baseRaw, &fields); err != nil {
			return err
		}
		changed, err := fingerprintFromMembers(key, previous.Source, fields)
		if err != nil {
			return err
		}
		sameEvidence := equalEvidence(evidence, previous.Evidence)
		if sameEvidence && changed == identity && bytes.Equal(evidence, previous.Evidence) {
			allMatch := true
			for field, value := range patch {
				if existing, exists := fields[field]; !exists || !bytes.Equal(existing, value) {
					allMatch = false
					break
				}
			}
			if allMatch {
				items.Set(key, previous)
				return nil
			}
		}
		for field, value := range patch {
			fields[field] = value
		}
		data, err := json.Marshal(fields)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, result); err != nil {
			return err
		}
		if !sameEvidence {
			count := new(big.Int)
			if previous.Occurrences != nil {
				count.Set((*big.Int)(previous.Occurrences))
			}
			count.Add(count, big.NewInt(1))
			result.Occurrences = (*OccurrenceCount)(count)
			result.LastSeenAt = new(seen)
		}
		if changed != identity {
			result.Read = new(false)
			result.Version = new(stamp)
		}
	} else {
		data, err := json.Marshal(patch)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, result); err != nil {
			return err
		}
		result.ID = key
		result.State = "pending"
		result.Read = new(false)
		result.Version = new(stamp)
		result.FirstSeenAt = new(stamp)
		result.LastSeenAt = new(seen)
		result.Occurrences = (*OccurrenceCount)(big.NewInt(1))
	}
	result.Evidence = evidence
	items.Set(key, result)
	return nil
}

// Dismiss marks only the exact versions displayed to the user as read.
func Dismiss(ctx context.Context, dataDir string, versions map[string]string) error {
	return UpdateItems(ctx, dataDir, func(items *Items) error {
		for key, version := range versions {
			item, _ := items.Get(key)
			if item != nil && item.Version != nil && *item.Version == version {
				value, err := dismissedItem(key, item)
				if err != nil {
					return err
				}
				items.Set(key, value)
			}
		}
		return nil
	})
}

func equalEvidence(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == 0 && len(b) == 0
	}
	left, err := jsonresp.Canonical(a)
	if err != nil {
		return false
	}
	right, err := jsonresp.Canonical(b)
	return err == nil && bytes.Equal(left, right)
}
