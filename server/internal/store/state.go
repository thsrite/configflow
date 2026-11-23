package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"

	"configflow/internal/faultprobe"
)

// Agent state (storage version 2, design/o17-runtime-state.md).
//
// The agent_state table holds the run-time state and histories of the Agents
// (deployments, rule-sync/DNS-follow/HA progress, ...) outside the
// configuration rows: one row per (agent id, member) with the member's JSON
// text. The store does not know which members those are; it keeps the rows
// opaque, commits them in the same transaction as the configuration rows and
// counts their changes separately (Head.State), so a state-only commit leaves
// the configuration generation unchanged.

const stateSchema = `CREATE TABLE agent_state (
        agent_id TEXT NOT NULL,
        member TEXT NOT NULL,
        value TEXT NOT NULL,
        revision INTEGER NOT NULL CHECK (revision >= 1),
        PRIMARY KEY (agent_id, member)
    ) WITHOUT ROWID`

var stateColumns = []string{"agent_id", "member", "value"}

// StateRow is one agent_state row: Value is the opaque JSON of member Member
// of the Agent Owner.
type StateRow struct {
	Owner  string
	Member string
	Value  json.RawMessage
}

type stateKey struct{ owner, member string }

// SameConfig reports whether h and o are the same configuration generation,
// whatever their agent state. Only caches whose values provably read no
// agent state member may compare heads with it; everything else compares
// whole heads with ==.
func (h Head) SameConfig(o Head) bool {
	return h.StoreID == o.StoreID && h.Generation == o.Generation && h.CommitID == o.CommitID
}

// targetState validates rows and returns them keyed, in (owner, member) order.
func targetState(rows []StateRow) (*ordered[stateKey, string], error) {
	sorted := make([]StateRow, len(rows))
	copy(sorted, rows)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Owner != sorted[j].Owner {
			return sorted[i].Owner < sorted[j].Owner
		}
		return sorted[i].Member < sorted[j].Member
	})
	result := newOrdered[stateKey, string]()
	for _, row := range sorted {
		if row.Owner == "" || row.Member == "" || !utf8.ValidString(row.Owner) || !utf8.ValidString(row.Member) {
			return nil, newError(ErrDocument, msgStateKey)
		}
		key := stateKey{row.Owner, row.Member}
		if result.has(key) {
			return nil, newError(ErrDocument, fmt.Sprintf(msgStateDuplicate, row.Owner, row.Member))
		}
		if !utf8.Valid(row.Value) || !json.Valid(row.Value) {
			return nil, newError(ErrDocument, fmt.Sprintf(msgStateInvalid, row.Owner, row.Member))
		}
		result.set(key, string(row.Value))
	}
	return result, nil
}

// hasStateTable reports whether the file has the agent_state table: a
// version-1 file has none until Upgrade.
func (c *Conn) hasStateTable() (bool, error) {
	names, err := c.strings1("SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'agent_state'")
	return len(names) > 0, err
}

// storedState reads the agent_state rows in primary key order; a file without
// the table has none.
func (c *Conn) storedState() (*ordered[stateKey, string], error) {
	result := newOrdered[stateKey, string]()
	present, err := c.hasStateTable()
	if err != nil || !present {
		return result, err
	}
	err = c.query("SELECT agent_id, member, value FROM agent_state ORDER BY agent_id, member", nil, func(row *sql.Rows) error {
		var key stateKey
		var value string
		if err := row.Scan(&key.owner, &key.member, &value); err != nil {
			return err
		}
		if err := checkRowText("agent_state", key.owner+"/"+key.member, stateColumns, key.owner, key.member, value); err != nil {
			return err
		}
		result.set(key, value)
		return nil
	})
	return result, err
}

// State returns the agent_state rows this connection sees, JSON-checked.
func (c *Conn) State() ([]StateRow, error) {
	stored, err := c.storedState()
	if err != nil {
		return nil, err
	}
	return stateList(stored)
}

func stateList(stored *ordered[stateKey, string]) ([]StateRow, error) {
	result := make([]StateRow, 0, len(stored.keys))
	for _, key := range stored.keys {
		value := stored.values[key]
		if err := checkRowJSON("agent_state", key.owner+"/"+key.member, value); err != nil {
			return nil, err
		}
		result = append(result, StateRow{Owner: key.owner, Member: key.member, Value: json.RawMessage(value)})
	}
	return result, nil
}

// stateChanges is what a commit writes to agent_state.
type stateChanges struct {
	delete []stateKey
	upsert []stateKey
}

func (s *stateChanges) any() bool { return len(s.delete)+len(s.upsert) > 0 }

func diffState(stored, target *ordered[stateKey, string]) *stateChanges {
	result := &stateChanges{}
	for _, key := range stored.keys {
		if !target.has(key) {
			result.delete = append(result.delete, key)
		}
	}
	for _, key := range target.keys {
		if value, ok := stored.get(key); !ok || value != target.values[key] {
			result.upsert = append(result.upsert, key)
		}
	}
	return result
}

func (c *Conn) applyState(pending *stateChanges, target *ordered[stateKey, string], revision int64) error {
	for _, key := range pending.delete {
		if err := c.exec("DELETE FROM agent_state WHERE agent_id = ? AND member = ?", key.owner, key.member); err != nil {
			return err
		}
	}
	for _, key := range pending.upsert {
		err := c.exec("INSERT INTO agent_state (agent_id, member, value, revision) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT (agent_id, member) DO UPDATE SET value = excluded.value, revision = excluded.revision",
			key.owner, key.member, target.values[key], revision)
		if err != nil {
			return err
		}
	}
	return nil
}

// -- storage version 1 → 2 ----------------------------------------------------

// Version returns the storage version of the file (Check validates it first).
func (s *Store) Version() (version int, err error) {
	err = s.Reading(func(connection *Conn) error {
		raw, err := connection.Meta("storage_version")
		if err != nil {
			return err
		}
		if json.Unmarshal(raw, &version) != nil || version < 1 {
			return newError(ErrCorrupt, msgVersionInvalid)
		}
		return nil
	})
	return version, err
}

// UpgradeTx is the open transaction of Upgrade.
type UpgradeTx struct {
	c        *Conn
	replaced []fieldKey
	state    []StateRow
}

// Field returns one stored field value, or present false.
func (t *UpgradeTx) Field(scope, owner, name string) (json.RawMessage, bool, error) {
	return t.c.Field(scope, owner, name)
}

// ReplaceField replaces the value of an existing field, keeping its position.
func (t *UpgradeTx) ReplaceField(scope, owner, name string, value json.RawMessage) error {
	if !utf8.Valid(value) || !json.Valid(value) {
		return newError(ErrDocument, fmt.Sprintf(msgInvalidField, scope, owner, name))
	}
	if err := t.c.exec("UPDATE fields SET value = ? WHERE scope = ? AND owner = ? AND field = ?", string(value), scope, owner, name); err != nil {
		return err
	}
	t.replaced = append(t.replaced, fieldKey{scope, owner, name})
	return nil
}

// AddState adds agent_state rows, written when the upgrade commits.
func (t *UpgradeTx) AddState(rows []StateRow) { t.state = append(t.state, rows...) }

// Upgrade brings a version-1 file to StorageVersion in one transaction: it
// creates the agent_state table, lets move rewrite fields and add state rows,
// and sets the version. A crash at any point leaves either the untouched
// version-1 file or the complete upgrade. A file already at StorageVersion is
// left alone (upgraded false). The caller backs the file up first.
func (s *Store) Upgrade(ctx context.Context, move func(*UpgradeTx) error) (upgraded bool, err error) {
	connection, err := s.connect(false)
	if err != nil {
		return false, translate(err)
	}
	defer connection.close()
	err = func() error {
		if err := connection.begin("BEGIN IMMEDIATE"); err != nil {
			return err
		}
		raw, err := connection.Meta("storage_version")
		if err != nil {
			return err
		}
		var version int
		if json.Unmarshal(raw, &version) != nil || version < 1 {
			return newError(ErrCorrupt, msgVersionInvalid)
		}
		if version >= StorageVersion {
			return nil
		}
		if version != 1 {
			return newError(ErrUnsupported, fmt.Sprintf(msgVersionUnsupported, string(raw), StorageVersion))
		}
		head, err := connection.Head()
		if err != nil {
			return err
		}
		if present, err := connection.hasStateTable(); err != nil {
			return err
		} else if !present {
			if err := connection.exec(stateSchema); err != nil {
				return err
			}
		}
		tx := &UpgradeTx{c: connection}
		if err := move(tx); err != nil {
			return err
		}
		target, err := targetState(tx.state)
		if err != nil {
			return err
		}
		for i, key := range target.keys {
			if s.BeforeUpgradeRow != nil {
				if err := s.BeforeUpgradeRow(i); err != nil {
					return err
				}
			}
			if err := connection.exec("INSERT INTO agent_state (agent_id, member, value, revision) VALUES (?, ?, ?, 1)", key.owner, key.member, target.values[key]); err != nil {
				return err
			}
		}
		meta := map[string]json.RawMessage{
			"storage_version":  jsonText(StorageVersion),
			"state_generation": jsonText(1),
			"state_commit_id":  jsonText(newUUIDHex()),
		}
		if len(tx.replaced) > 0 {
			// The configuration rows changed bytes: a new generation, as any commit.
			head.Generation++
			meta["generation"] = jsonText(head.Generation)
			meta["commit_id"] = jsonText(newUUIDHex())
			for _, key := range tx.replaced {
				if err := connection.exec("UPDATE fields SET revision = ? WHERE scope = ? AND owner = ? AND field = ?", head.Generation, key.scope, key.owner, key.field); err != nil {
					return err
				}
			}
		}
		for key, value := range meta {
			if err := connection.exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, string(value)); err != nil {
				return err
			}
		}
		if s.BeforeCommit != nil {
			if err := s.BeforeCommit(); err != nil {
				return err
			}
		}
		faultprobe.Point(ctx, faultprobe.StoreBeforeCommit)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := connection.commit(); err != nil {
			return err
		}
		upgraded = true
		return nil
	}()
	if err != nil {
		return false, translate(err)
	}
	return upgraded, nil
}

// stateMetaFrom decodes the state counters of a head; a version-1 file has none.
func stateMetaFrom(meta map[string]json.RawMessage) (generation int64, commitID string, err error) {
	if value := meta["state_generation"]; len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		if json.Unmarshal(value, &generation) != nil || generation < 0 {
			return 0, "", newError(ErrCorrupt, msgStateHeadInvalid)
		}
	}
	if value := meta["state_commit_id"]; len(value) > 0 && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		if !decodeString(value, &commitID) {
			return 0, "", newError(ErrCorrupt, msgStateHeadInvalid)
		}
	}
	return generation, commitID, nil
}
