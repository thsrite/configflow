package agents

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/store"
)

// Borrowed records. ReadAgents, ReadAgent, ReadAgentByToken, ListAgents,
// AgentSnapshots, ReadHAPair and AgentPair return records borrowed from the
// repository's immutable registry generation (repo.BorrowAgents) instead of
// decoding a private copy of every Agent:
//
//   - Each returned record is a shallow copy of the stored record. The caller
//     may reassign its top-level fields (Status, LastHeartbeat, ...), and
//     SetJSON, Clear, Map.Set and Fields.Set are copy-on-write.
//   - Everything reachable through pointers, Map values and nested slices is
//     shared with every other reader and must not be modified. Copy the nested
//     struct before changing it, or use CloneAgent.
//   - The same holds for values taken out of a borrowed record: a Deployment
//     from Deployments (or LatestDeployment, ReadDeploymentModel) is a struct
//     copy whose Events, PreparedContent and HAPublication are still shared;
//     SetJSON/Clear on that copy are copy-on-write, in-place edits are not.
//   - A returned slice is the caller's own: its elements may be reassigned.
//   - Records may be retained; they never change after they are returned.
//   - A record handed to a write transaction, or retained and changed in depth,
//     must be a CloneAgent copy.
//
// Inside a write transaction the working records may still change, so those
// reads keep deep-copying the selected records.

// ReadAgents returns borrowed registry records (see "Borrowed records"),
// including newer live heartbeats. The repository's cached model is read once;
// unrelated profiles are never serialized or decoded here. inspect sees the
// original stored record before the history projection and must neither mutate
// nor retain it.
func (m *Manager) ReadAgents(ctx context.Context, history bool, inspect func(*model.Agent)) ([]model.Agent, error) {
	records, shared, err := m.repository.BorrowAgents(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]model.Agent, 0, len(records))
	for i := range records {
		original := &records[i]
		if inspect != nil {
			inspect(original)
		}
		record, err := readerAgent(original, history, shared)
		if err != nil {
			return nil, err
		}
		result = append(result, record)
	}
	return m.runtime.overlayModels(result), nil
}

// ReadAgent returns one borrowed record (see "Borrowed records") with its live
// heartbeat, or nil.
func (m *Manager) ReadAgent(ctx context.Context, id string, history bool) (*model.Agent, error) {
	records, shared, err := m.repository.BorrowAgents(ctx)
	if err != nil {
		return nil, err
	}
	source := registryAgent(records, id)
	if source == nil {
		return nil, nil
	}
	record, err := readerAgent(source, history, shared)
	if err != nil {
		return nil, err
	}
	return &m.runtime.overlayModels([]model.Agent{record})[0], nil
}

// Generation identifies the registry generation a borrowed read came from.
// Shared is false inside a write transaction, whose working records may still
// change: data derived from such a read must not be kept. Head is the store
// head of a shared generation (the zero Head otherwise).
type Generation struct {
	Head   store.Head
	Shared bool
}

// ReadAgentsAt is ReadAgents that also reports the generation it read.
func (m *Manager) ReadAgentsAt(ctx context.Context, history bool, inspect func(*model.Agent)) ([]model.Agent, Generation, error) {
	records, head, shared, err := m.repository.BorrowAgentsAt(ctx)
	if err != nil {
		return nil, Generation{}, err
	}
	result := make([]model.Agent, 0, len(records))
	for i := range records {
		original := &records[i]
		if inspect != nil {
			inspect(original)
		}
		record, err := readerAgent(original, history, shared)
		if err != nil {
			return nil, Generation{}, err
		}
		result = append(result, record)
	}
	return m.runtime.overlayModels(result), Generation{head, shared}, nil
}

// ReadAgentAt is ReadAgent that also reports the generation it read and the
// record's position in that generation's registry (-1 when not found).
func (m *Manager) ReadAgentAt(ctx context.Context, id string, history bool) (*model.Agent, int, Generation, error) {
	records, head, shared, err := m.repository.BorrowAgentsAt(ctx)
	if err != nil {
		return nil, -1, Generation{}, err
	}
	for i := range records {
		if records[i].ID != id || !agentIDProven(&records[i]) {
			continue
		}
		record, err := readerAgent(&records[i], history, shared)
		if err != nil {
			return nil, -1, Generation{}, err
		}
		return &m.runtime.overlayModels([]model.Agent{record})[0], i, Generation{head, shared}, nil
	}
	return nil, -1, Generation{head, shared}, nil
}

// ReadAgentByToken selects one original string token without encoding unrelated
// Agents or their histories. The result is borrowed (see "Borrowed records").
func (m *Manager) ReadAgentByToken(ctx context.Context, token string) (*model.Agent, error) {
	records, shared, err := m.repository.BorrowAgents(ctx)
	if err != nil {
		return nil, err
	}
	for i := range records {
		a := &records[i]
		if a.Token != token {
			continue
		}
		if original, valid := storedText(a.MemberJSON("token")); valid && original == token {
			record, err := readerAgent(a, true, shared)
			if err != nil {
				return nil, err
			}
			return &m.runtime.overlayModels([]model.Agent{record})[0], nil
		}
	}
	return nil, nil
}

// readerAgent is the record a reader receives: a shallow copy of a shared
// generation, or a deep copy of a write transaction's working record.
func readerAgent(source *model.Agent, history, shared bool) (model.Agent, error) {
	if !shared {
		copied, err := copyAgent(source, history)
		if err != nil {
			return model.Agent{}, err
		}
		return *copied, nil
	}
	return projectAgent(source, history)
}

// projectAgent is a shallow copy of source, without its histories unless
// history is set. Raw/Fields are copy-on-write: Clear affects only the copy.
func projectAgent(source *model.Agent, history bool) (model.Agent, error) {
	selected := *source
	if !history {
		for _, field := range repo.AgentHistoryFields {
			if err := selected.Clear(field); err != nil {
				return model.Agent{}, err
			}
		}
	}
	return selected, nil
}

// CloneAgent returns a deep, independently owned copy of record, histories
// included: an encode and decode of the whole record. Use it only where a
// borrowed record is handed to a write transaction or changed in depth.
func CloneAgent(record *model.Agent) (*model.Agent, error) {
	return copyAgent(record, true)
}

func copyAgent(source *model.Agent, history bool) (*model.Agent, error) {
	selected, err := projectAgent(source, history)
	if err != nil {
		return nil, err
	}
	data, err := model.Marshal(selected)
	if err != nil {
		return nil, fmt.Errorf("copy Agent: %w", err)
	}
	var result model.Agent
	if err = json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("copy Agent: %w", err)
	}
	return &result, nil
}

// EditAgents owns a typed registry transaction. The callback can inspect and
// change the working records, but must not retain them after returning. The
// repository checks cancellation again immediately before its durable commit.
func (m *Manager) EditAgents(ctx context.Context, profileID string, edit func(context.Context, *[]model.Agent) error) error {
	return m.editRegistry(ctx, profileID, func(tx context.Context, system *model.System) error { return edit(tx, &system.Agents) })
}

func (m *Manager) editRegistry(ctx context.Context, profileID string, edit func(context.Context, *model.System) error) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if testFailUpdate != nil {
		if err := testFailUpdate(); err != nil {
			return err
		}
	}
	err := m.repository.UpdateSystem(ctx, func(tx context.Context, system *model.System) error {
		if err := tx.Err(); err != nil {
			return err
		}
		if profileID != "" {
			if _, err := m.repository.ProfileMetadataRecord(tx, profileID); err != nil {
				return err
			}
		}
		if err := edit(tx, system); err != nil {
			return err
		}
		return tx.Err()
	})
	if errors.Is(err, repo.ErrInUse) {
		return &DeploymentConflict{Message: err.Error()}
	}
	return err
}

func registryAgent(list []model.Agent, id string) *model.Agent {
	for i := range list {
		if list[i].ID == id && agentIDProven(&list[i]) {
			return &list[i]
		}
	}
	return nil
}

// ObserveHeartbeat records volatile liveness and persists only changed reported
// versions. The lookup uses a cached read view and copies seven runtime fields,
// so a heartbeat does not decode the Agent list or configuration document.
func (m *Manager) ObserveHeartbeat(ctx context.Context, id string, payload *model.Agent) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	reported, err := reportedHeartbeat(payload)
	if err != nil {
		return false, err
	}
	return m.observeHeartbeat(ctx, id, reported)
}

func (m *Manager) observeHeartbeat(ctx context.Context, id string, payload heartbeatPayload) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return false, err
	}
	mark := m.runtime.Mark()
	var source *heartbeatSource
	// Reported values are compared with the stored JSON, like Python's
	// heartbeat[key] != agent.get(key): a numeric, null or missing stored
	// member differs from reported text even when its projection matches.
	type storedMember struct {
		raw     json.RawMessage
		present bool
	}
	stored := map[string]storedMember{}
	err := m.repository.ViewAgents(ctx, func(records []model.Agent) error {
		record := registryAgent(records, id)
		if record == nil {
			return nil
		}
		fields, err := runtimeProjection(record)
		if err != nil {
			return err
		}
		for _, key := range []string{"version", "config_version"} {
			raw, present, err := record.MemberJSON(key)
			if err != nil {
				return err
			}
			stored[key] = storedMember{raw, present}
		}
		source = &heartbeatSource{record.ID, record.Name, record.LastHeartbeat, fields}
		return nil
	})
	if err != nil || source == nil {
		return false, err
	}
	var recorded bool
	if testRecordHeartbeat != nil {
		recorded, err = testRecordHeartbeat(ctx, m, *source, payload, mark)
	} else {
		recorded, err = m.runtime.recordFields(ctx, *source, payload, mark)
	}
	if err != nil {
		return false, err
	}
	if !recorded {
		return false, nil
	}
	changes := make(map[string]json.RawMessage, 2)
	for _, item := range []struct {
		key string
		raw json.RawMessage
	}{{"version", payload.Version}, {"config_version", payload.ConfigVersion}} {
		if item.raw != nil && !sameStoredJSON(stored[item.key].raw, stored[item.key].present, item.raw) {
			changes[item.key] = item.raw
		}
	}
	if len(changes) > 0 {
		saved := false
		err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
			record := registryAgent(*records, id)
			if record == nil {
				return errNothingToSave
			}
			for key, data := range changes {
				if err := record.SetJSON(key, data); err != nil {
					return err
				}
			}
			saved = true
			return tx.Err()
		})
		if err != nil && !errors.Is(err, errNothingToSave) {
			return false, err
		}
		if !saved {
			return false, nil
		}
	}
	if payload.SystemMetrics != nil {
		if err := m.addReportedMetrics(ctx, id, payload.SystemMetrics); err != nil {
			m.logMetricsFailure(ctx, id, source.name, err)
		}
	}
	return true, nil
}

// sameStoredJSON compares a reported member with the original stored member.
// Strings compare by value; other JSON compares by its exact token.
func sameStoredJSON(stored json.RawMessage, present bool, reported json.RawMessage) bool {
	if !present {
		return bytes.Equal(bytes.TrimSpace(reported), []byte("null"))
	}
	// Exact tokens are equal without allocating decoded string copies. Escaped
	// spellings still fall through to the value comparison below.
	if bytes.Equal(bytes.TrimSpace(stored), bytes.TrimSpace(reported)) {
		return true
	}
	left, leftOK := storedText(stored, true, nil)
	right, rightOK := storedText(reported, true, nil)
	if leftOK || rightOK {
		return leftOK && rightOK && left == right
	}
	return bytes.Equal(bytes.TrimSpace(stored), bytes.TrimSpace(reported))
}

// agentIDProven is an identity check, not stored scalar coercion. R5/text
// fallback cannot turn an originally numeric identifier into authentication.
func agentIDProven(record *model.Agent) bool {
	raw, present, err := record.MemberJSON("id")
	if err != nil || !present {
		return false
	}
	var original string
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &original) == nil && original == record.ID
}

func (m *Manager) logMetricsFailure(ctx context.Context, id, name string, failure error) {
	if name == "" {
		name = id
	}
	// Credential traversal is error-only. The steady heartbeat path never walks
	// histories or the configuration to prepare a log it will not emit.
	var secrets redact.Secrets
	err := m.repository.ViewAgents(ctx, func(records []model.Agent) error {
		if record := registryAgent(records, id); record != nil {
			raw, err := model.Marshal(record)
			if err != nil {
				return err
			}
			secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
		}
		return nil
	})
	view := struct {
		Name  string `json:"name"`
		Error string `json:"error"`
	}{name, failure.Error()}
	if err != nil {
		view.Name, view.Error = redact.Redacted, redact.Redacted
	} else {
		raw, encodeErr := json.Marshal(view)
		if encodeErr != nil {
			view.Name, view.Error = redact.Redacted, redact.Redacted
		} else if err := jsondecode.Unmarshal(redact.SanitizeCredentials(json.RawMessage(raw), secrets), &view); err != nil {
			view.Name, view.Error = redact.Redacted, redact.Redacted
		}
	}
	logger.Warningf(msgMetricsHistoryFailure, logx.LogText(view.Name, 120), logx.LogText(view.Error, DetailLimit))
}

// AgentEnabledModel defaults an omitted automation switch to on. An explicit
// null or R5 fallback is off; it must not reactivate an imported disabled node.
func AgentEnabledModel(record *model.Agent) bool {
	if record == nil {
		return false
	}
	if record.Enabled != nil {
		return *record.Enabled
	}
	_, present, err := record.MemberJSON("enabled")
	return err == nil && !present
}

// ResolveAgentProfile resolves an optional original string binding. Stored
// scalar projections cannot select configuration or establish task ownership.
func (m *Manager) ResolveAgentProfile(ctx context.Context, a *model.Agent) (string, error) {
	if a == nil {
		return "", repo.NewValidationError("Invalid profile id")
	}
	return m.agentProfileID(ctx, a)
}

// saveReportedModels atomically persists import patches on original string IDs.
func (m *Manager) saveReportedModels(ctx context.Context, changes map[string]model.Fields) (map[string]bool, error) {
	saved := map[string]bool{}
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		for i := range *records {
			a := &(*records)[i]
			patch, found := changes[a.ID]
			if !found || !agentIDProven(a) {
				continue
			}
			for key, raw := range patch.All() {
				if err := a.SetJSON(key, raw); err != nil {
					return err
				}
			}
			saved[a.ID] = true
		}
		if len(saved) == 0 {
			return errNothingToSave
		}
		return nil
	})
	if errors.Is(err, errNothingToSave) {
		return saved, nil
	}
	if err != nil {
		return map[string]bool{}, err
	}
	return saved, nil
}

// AgentMatchesID verifies a registry identity from its original string token.
func AgentMatchesID(record *model.Agent, id string) bool {
	return record != nil && record.ID == id && agentIDProven(record)
}

// ListAgents returns borrowed live records (see "Borrowed records") with
// freshness-derived listing status. ReadAgent retains the stored status for
// individual reads.
func (m *Manager) ListAgents(ctx context.Context, history bool) ([]model.Agent, error) {
	records, err := m.ReadAgents(ctx, history, nil)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if !FreshHeartbeat(records[i].LastHeartbeat, nil) {
			records[i].Status = "offline"
		}
	}
	return records, nil
}

// AgentSnapshots returns borrowed listing records (see "Borrowed records") as
// pointers for consumers that retain them.
func (m *Manager) AgentSnapshots(ctx context.Context, history bool) ([]*model.Agent, error) {
	rows, err := m.ListAgents(ctx, history)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Agent, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

func (m *Manager) addReportedMetrics(ctx context.Context, id string, raw json.RawMessage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if hook := currentHooks().AddMetricsJSON; hook != nil {
		return hook(ctx, m, id, raw)
	}
	return nil
}

// AgentPair returns borrowed reciprocal HA members (see "Borrowed records") for
// pointer-based consumers.
func (m *Manager) AgentPair(ctx context.Context, id string) ([]*model.Agent, error) {
	rows, err := m.ReadHAPair(ctx, id)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Agent, len(rows))
	for i := range rows {
		out[i] = &rows[i]
	}
	return out, nil
}

// RuleSyncAgent is the narrow per-Agent view read by rule-sync scheduling and
// presence tracking: identity, service type, sync switches, live presence
// metadata and the credentials found in the stored record, without copying
// deployment or IPv6 verification histories.
type RuleSyncAgent struct {
	ID              string
	ServiceType     string
	RuleSyncLibrary bool
	RuleSyncRules   bool
	// Credentials are collected from the stored record with histories
	// cleared, the same members the previous owned listing copy encoded.
	Credentials redact.Secrets
	// CredentialsErr reports a record that could not be encoded; presence
	// display must then be withheld.
	CredentialsErr error
	Presence       PresenceRecord
}

// RuleSyncAgents reads only the scheduling and presence fields of each
// registered Agent from the cached registry view, overlaying live heartbeat
// freshness, without decoding owned copies or encoding histories.
func (m *Manager) RuleSyncAgents(ctx context.Context) ([]RuleSyncAgent, error) {
	var out []RuleSyncAgent
	err := m.repository.ViewAgents(ctx, func(records []model.Agent) error {
		live := m.runtime.liveHeartbeats(records)
		out = make([]RuleSyncAgent, 0, len(records))
		for i := range records {
			a := &records[i]
			liveHB, hasLive := live[i]
			var library, rules bool
			if a.RuleSync != nil {
				library = a.RuleSync.Settings.Library != nil && *a.RuleSync.Settings.Library
				rules = a.RuleSync.Settings.Rules != nil && *a.RuleSync.Settings.Rules
			}
			credentials, credentialsErr := storedCredentials(a)
			out = append(out, RuleSyncAgent{
				ID:              a.ID,
				ServiceType:     a.ServiceType,
				RuleSyncLibrary: library,
				RuleSyncRules:   rules,
				Credentials:     credentials,
				CredentialsErr:  credentialsErr,
				Presence:        presenceRecordOf(a, liveHB, hasLive, true),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// storedCredentials collects credential values of one stored record without
// its histories. Raw/Fields are copy-on-write: Clear affects only the local
// value, never the cached registry record.
func storedCredentials(source *model.Agent) (redact.Secrets, error) {
	selected := *source
	for _, field := range repo.AgentHistoryFields {
		if err := selected.Clear(field); err != nil {
			return nil, err
		}
	}
	data, err := model.Marshal(selected)
	if err != nil {
		return nil, fmt.Errorf("encode Agent credentials: %w", err)
	}
	return redact.CollectCredentials(json.RawMessage(data), nil), nil
}

// AgentName returns only an original stored string name, including an explicitly
// empty string. Stored scalar projections are not a replacement for this evidence.
func AgentName(a *model.Agent) (string, bool) {
	if a == nil {
		return "", false
	}
	return storedText(a.MemberJSON("name"))
}
