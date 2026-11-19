package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// ReadAgents returns caller-owned registry records, including newer live
// heartbeats. The repository's cached model is read once; unrelated profiles
// are never serialized or decoded here. inspect sees the original stored record
// before the history projection and must neither mutate nor retain it.
func (m *Manager) ReadAgents(ctx context.Context, history bool, inspect func(*model.Agent)) ([]model.Agent, error) {
	var result []model.Agent
	err := m.repository.View(ctx, func(document *model.Document) error {
		result = make([]model.Agent, 0, len(document.System.Agents))
		for i := range document.System.Agents {
			original := &document.System.Agents[i]
			if inspect != nil {
				inspect(original)
			}
			record, err := copyAgent(original, history)
			if err != nil {
				return err
			}
			result = append(result, *record)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return m.runtime.overlayModels(result), nil
}

// ReadAgent returns one caller-owned record with its live heartbeat, or nil.
// It copies only the selected Agent, even when the registry is large.
func (m *Manager) ReadAgent(ctx context.Context, id string, history bool) (*model.Agent, error) {
	var result *model.Agent
	err := m.repository.View(ctx, func(document *model.Document) error {
		record := registryAgent(document.System.Agents, id)
		if record != nil {
			var err error
			result, err = copyAgent(record, history)
			return err
		}
		return nil
	})
	if err != nil || result == nil {
		return result, err
	}
	return &m.runtime.overlayModels([]model.Agent{*result})[0], nil
}

func copyAgent(source *model.Agent, history bool) (*model.Agent, error) {
	// Raw/Fields are copy-on-write. Clear affects this local value, never source.
	selected := *source
	if !history {
		if err := selected.Clear("deployments"); err != nil {
			return nil, err
		}
		if err := selected.Clear("ipv6_verifications"); err != nil {
			return nil, err
		}
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
			if _, err := m.repository.ProfileMetadata(tx, profileID); err != nil {
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
	return m.observeHeartbeat(ctx, id, reported, payload)
}

func (m *Manager) observeHeartbeat(ctx context.Context, id string, payload heartbeatPayload, testPayload *model.Agent) (bool, error) {
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
	err := m.repository.View(ctx, func(document *model.Document) error {
		record := registryAgent(document.System.Agents, id)
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
		record, err := source.fields.object()
		if err == nil {
			record.Set("id", source.id)
			record.Set("name", source.name)
			record.Set("last_heartbeat", source.lastHeartbeat)
		}
		if err != nil {
			return false, err
		}
		var fields *jsonx.Object
		if testPayload != nil {
			fields, err = agentObject(testPayload)
		} else {
			raw, encodeErr := json.Marshal(payload)
			if encodeErr != nil {
				return false, encodeErr
			}
			fields, err = decodeHeartbeatObject(raw)
		}
		if err != nil {
			return false, err
		}
		recorded = testRecordHeartbeat(m, record, fields, mark)
	} else {
		recorded, err = m.runtime.recordFields(ctx, *source, payload, mark)
		if err != nil {
			return false, err
		}
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
	err := m.repository.View(ctx, func(document *model.Document) error {
		if record := registryAgent(document.System.Agents, id); record != nil {
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
		} else if err := json.Unmarshal(redact.SanitizeCredentials(json.RawMessage(raw), secrets), &view); err != nil {
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
