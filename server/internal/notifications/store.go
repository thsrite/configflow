package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"configflow/internal/jsonx"
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

// Fingerprint identifies an incident, not a polling result.
func Fingerprint(key string, item *jsonx.Object) string {
	var fields []string
	if item.Value("source") == "router-check" && strings.HasSuffix(key, ":changes") {
		fields = []string{"signature"}
	} else {
		fields = []string{"title", "message", "severity", "status_label", "target"}
		if item.Value("source") == "router-check" {
			fields = append(fields, "signature")
		} else {
			fields = append(fields, "evidence")
		}
	}
	value := jsonx.NewObject()
	for _, field := range fields {
		value.Set(field, item.Value(field))
	}
	data, _ := jsonx.Encode(value, jsonx.Options{SortKeys: true})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// dismissed keeps only what avoids replaying an acknowledged receipt.
func dismissed(key string, item *jsonx.Object) *jsonx.Object {
	return jsonx.Obj("state", "dismissed", "fingerprint", Fingerprint(key, item), "source", item.Value("source"))
}

// Transaction is notification_store.transaction(data_dir): fn receives the
// items (key → item) and may change them; the file is rewritten when the
// document changed. Old read or resolved records are migrated on first
// access. A file that does not decode returns the decoder error (ValueError).
func Transaction(dataDir string, fn func(items *jsonx.Object) error) error {
	return TransactionContext(context.Background(), dataDir, fn)
}

// TransactionContext owns a notification leaf update. Request cancellation is
// detached, but the originating Runtime still cancels admission and replacement.
func TransactionContext(ctx context.Context, dataDir string, fn func(items *jsonx.Object) error) error {
	return transaction(lifecycle.Detach(ctx), dataDir, 0, true, func(items *jsonx.Object, _ bool) error { return fn(items) })
}

func transaction(ctx context.Context, dataDir string, ticket uint64, persist bool, fn func(items *jsonx.Object, apply bool) error) error {
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

	var data *jsonx.Object
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		data = jsonx.Obj("items", jsonx.NewObject())
	case err != nil:
		return err
	default:
		value, err := jsonx.Parse(raw)
		if err != nil {
			return err
		}
		object, ok := value.(*jsonx.Object)
		if !ok {
			return ErrInvalidStore
		}
		if _, ok := object.Value("items").(*jsonx.Object); !ok {
			return ErrInvalidStore
		}
		data = object
	}
	before := jsonx.DeepCopy(data)
	items := data.GetObject("items")
	migrated := jsonx.NewObject()
	for key, value := range items.All() {
		item, ok := value.(*jsonx.Object)
		if !ok {
			return errors.New("'" + pyTypeName(value) + "' object has no attribute 'get'")
		}
		state := item.Value("state")
		if state != "pending" && state != "dismissed" {
			continue
		}
		if jsonx.Truthy(item.Value("read")) {
			migrated.Set(key, dismissed(key, item))
		} else {
			migrated.Set(key, item)
		}
	}
	data.Set("items", migrated)
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
	if jsonx.Equal(data, before) {
		if ticket != 0 && apply {
			appliedVersions[key] = ticket
		}
		return nil
	}
	encoded, err := jsonx.Compact(data)
	if err != nil {
		return err
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

// Observe is notification_store.observe: keep unread incidents; "unknown"
// does not count as recovery, "resolved"/"inactive" remove the item. The
// observation's "state" and "evidence" are consumed.
func Observe(items *jsonx.Object, key string, observation *jsonx.Object) {
	status, _ := observation.Pop("state")
	previous, _ := items.Value(key).(*jsonx.Object)
	if status == "unknown" {
		return
	}
	if status == "resolved" || status == "inactive" {
		items.Delete(key)
		return
	}
	identity := Fingerprint(key, observation)
	if previous != nil && previous.Value("state") == "dismissed" && previous.Value("fingerprint") == identity {
		return
	}
	stamp := Now()
	evidence, ok := observation.Pop("evidence")
	if !ok {
		evidence = ""
	}
	seen := observation.Value("checked_at")
	if !jsonx.Truthy(seen) {
		seen = stamp
	}
	if previous != nil && previous.Value("state") == "pending" {
		changed := Fingerprint(key, previous) != identity
		previous.Update(observation)
		if !jsonx.Equal(evidence, previous.Value("evidence")) {
			occurrences, _ := jsonx.AsInt(previous.Value("occurrences"))
			previous.Set("occurrences", occurrences+1)
			previous.Set("last_seen_at", seen)
		}
		if changed {
			previous.Set("read", false)
			previous.Set("version", stamp)
		}
		previous.Set("evidence", evidence)
		return
	}
	item := observation.Clone()
	item.Update(jsonx.Obj("id", key, "state", "pending", "read", false, "version", stamp,
		"first_seen_at", stamp, "last_seen_at", seen, "occurrences", 1, "evidence", evidence))
	items.Set(key, item)
}

// MarkRead is notification_store.mark_read: dismiss the items whose version
// is the one the user saw.
func MarkRead(dataDir string, versions *jsonx.Object) error {
	return MarkReadContext(context.Background(), dataDir, versions)
}

// MarkReadContext dismisses matching versions in a shutdown-aware transaction.
func MarkReadContext(ctx context.Context, dataDir string, versions *jsonx.Object) error {
	return TransactionContext(ctx, dataDir, func(items *jsonx.Object) error {
		for key, version := range versions.All() {
			item, ok := items.Value(key).(*jsonx.Object)
			if ok && jsonx.Equal(item.Value("version"), version) {
				items.Set(key, dismissed(key, item))
			}
		}
		return nil
	})
}

func pyTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case string:
		return "str"
	case bool:
		return "bool"
	case []any:
		return "list"
	case jsonx.Number:
		return "int"
	}
	return "object"
}
