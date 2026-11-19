package rulesync

// This file alone adapts legacy repository/Agent records. Reconciliation and
// projection use typed native JSON, immutable deploy.Source and yamlx nodes.
import (
	"configflow/internal/agents/monitor"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

func decodeAgent(record *agents.Record) (*model.Agent, error) {
	raw, err := json.Marshal(agentView(record))
	if err != nil {
		return nil, fmt.Errorf("encode agent snapshot: %w", err)
	}
	var a model.Agent
	if err = json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("decode agent snapshot: %w", err)
	}
	return &a, nil
}

// SettingsFor reads strictly boolean scopes; historical wrong types are off.
func SettingsFor(record *agents.Record) Settings {
	sync := record.GetObject("rule_sync")
	if sync == nil {
		return Settings{}
	}
	raw, err := json.Marshal(sync.Value("settings"))
	if err != nil {
		return Settings{}
	}
	var s model.RuleSyncSettings
	if json.Unmarshal(raw, &s) != nil {
		return Settings{}
	}
	return Settings{Library: s.Library != nil && *s.Library, Rules: s.Rules != nil && *s.Rules}
}

// agentView deliberately omits history members; none of this package's typed
// reads needs them, and the light-read sentinel must never enter a JSON codec.
func agentView(record *agents.Record) *jsonx.Object {
	out := jsonx.NewObject()
	if record == nil {
		return out
	}
	for key, v := range record.All() {
		if !slices.Contains(repo.AgentHistoryFields, key) {
			out.Set(key, v)
		}
	}
	return out
}

func publishSource(ctx context.Context, m *agents.Manager, profileID string) (*deploy.Source, error) {
	return repositorySource(ctx, m.Repository(), profileID)
}

func persistSync(ctx context.Context, m *agents.Manager, id string, settings *Settings, state *model.RuleSyncState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return m.EditAgents(ctx, "", func(tx context.Context, list *[]model.Agent) error {
		for i := range *list {
			record := &(*list)[i]
			if !agents.AgentMatchesID(record, id) {
				continue
			}
			if settings != nil && agents.HAEnablingModel(record) {
				return refuse(msgPairEnabling)
			}
			sync := model.AgentRuleSync{}
			if record.RuleSync != nil {
				sync = *record.RuleSync
			}
			if settings != nil {
				sync.Settings = model.RuleSyncSettings{Library: &settings.Library, Rules: &settings.Rules}
			}
			if state != nil {
				data, err := encodeState(state)
				if err != nil {
					return err
				}
				var next model.RuleSyncState
				if err = json.Unmarshal(data, &next); err != nil {
					return err
				}
				sync.State = &next
			}
			record.RuleSync = &sync
			return tx.Err()
		}
		return ErrAgentNotFound
	})
}

func renderCandidate(ctx context.Context, target deploy.Target, source *deploy.Source, baseURL string) (content string, downloads []convert.Download, err error) {
	err = source.ViewConfig(func(config *model.Config) error {
		if target.ServiceType == "mihomo" {
			var e error
			content, e = convert.MihomoConfig(ctx, config, convert.MihomoOptions{BaseURL: baseURL, SyncLANHosts: true, SkipProviderPreflight: true, ForAgent: true, RegionProbe: target.DomainDiscovery})
			return e
		}
		profile, e := oxidns.NewTypedProfile(config)
		if e != nil {
			return e
		}
		content, e = oxidns.Generate(profile, baseURL)
		if e != nil {
			return e
		}
		entries, e := oxidns.RulesetDownloads(profile, baseURL)
		if e != nil {
			return e
		}
		downloads = make([]convert.Download, 0, len(entries))
		for _, entry := range entries {
			downloads = append(downloads, convert.Download{URL: entry.URL, LocalPath: entry.LocalPath})
		}
		return nil
	})
	return content, downloads, err
}

func recordID(r *agents.Record) string { return r.GetString("id") }
func recordSupported(r *agents.Record) bool {
	return r.GetString("service_type") == "mihomo" || r.GetString("service_type") == "oxidns"
}
func compareLeader(a, b *agents.Record) int {
	ap := a.GetObject("oxidns_ha").GetString("role") == "primary"
	bp := b.GetObject("oxidns_ha").GetString("role") == "primary"
	if ap != bp {
		if ap {
			return -1
		}
		return 1
	}
	return strings.Compare(a.GetString("id"), b.GetString("id"))
}
func configurePair(ctx context.Context, m *agents.Manager, members []*agents.Record, s Settings) error {
	expected := make([]*model.Agent, 0, len(members))
	for _, member := range members {
		a, err := decodeAgent(member)
		if err != nil {
			return err
		}
		if !agents.AgentMatchesID(a, a.ID) {
			return refuse(msgPairChanged)
		}
		expected = append(expected, a)
	}
	if len(expected) != 2 || !agents.HAPairMatchesModel(expected[0], expected[1]) {
		return refuse(msgPairChanged)
	}
	return m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		selected := make([]*model.Agent, 0, len(expected))
		for _, member := range expected {
			var current *model.Agent
			for i := range *records {
				if agents.AgentMatchesID(&(*records)[i], member.ID) {
					current = &(*records)[i]
					break
				}
			}
			if current == nil {
				return refuse(msgPairChanged)
			}
			left, err := yamlx.FromJSON(rawJSON(current.OxiDNSHA))
			if err != nil {
				return err
			}
			right, err := yamlx.FromJSON(rawJSON(member.OxiDNSHA))
			if err != nil {
				return err
			}
			if !yamlx.Equal(left, right) {
				return refuse(msgPairChanged)
			}
			if agents.HAEnablingModel(current) {
				return refuse(msgPairEnabling)
			}
			selected = append(selected, current)
		}
		if !agents.HAPairMatchesModel(selected[0], selected[1]) {
			return refuse(msgPairChanged)
		}
		for _, current := range selected {
			sync := model.AgentRuleSync{}
			if current.RuleSync != nil {
				sync = *current.RuleSync
			}
			sync.Settings = model.RuleSyncSettings{Library: &s.Library, Rules: &s.Rules}
			current.RuleSync = &sync
		}
		return tx.Err()
	})
}
func rawJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func safeText(ctx context.Context, m *agents.Manager, record *agents.Record, text string) string {
	system, err := m.Repository().GetSystemSettings(ctx)
	if err != nil {
		return "[REDACTED]"
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, system)
	if err != nil {
		return "[REDACTED]"
	}
	secrets = redact.CollectCredentials(record, secrets)
	return safeDisplay(text, secrets)
}
func logState(ctx context.Context, m *agents.Manager, record *agents.Record, status, message string) {
	name := record.GetString("name")
	if name == "" {
		name = record.GetString("id")
	}
	name = safeText(ctx, m, record, name)
	name = logx.LogText(name, 120)
	message = logx.LogText(message, 300)
	switch status {
	case "running", "succeeded":
		logger.Infof("Agent %s 规则自动同步：%s", name, message)
	case "failed", "blocked":
		logger.Warningf("Agent %s 规则自动同步：%s", name, message)
	default:
		logger.Debugf("Agent %s 规则自动同步：%s", name, message)
	}
}
func taskRow(ctx context.Context, record *agents.Record, statuses *jsonx.Object) (*jsonx.Object, error) {
	if !recordSupported(record) {
		return nil, nil
	}
	id := recordID(record)
	key := "agent_rule_sync:" + id
	enabled := SettingsFor(record).Any()
	if !enabled && !statuses.Has(key) {
		return nil, nil
	}
	a, e := decodeAgent(record)
	if e != nil {
		return nil, e
	}
	state := stateOf(a)
	history, e := tasks.History(statuses.GetObject(key))
	if e != nil {
		return nil, e
	}
	message := state.Message
	m, e := agents.Get(ctx)
	if e != nil {
		return nil, e
	}
	message = safeText(ctx, m, record, message)
	if enabled && state.Status == "blocked" {
		history.Set("last_status", "skipped")
		history.Set("last_error", message)
	} else if enabled && (state.Status == "succeeded" || state.Status == "failed") {
		if state.Status == "succeeded" {
			history.Set("last_status", "success")
			history.Set("last_error", "")
		} else {
			history.Set("last_status", "failed")
			history.Set("last_error", message)
		}
	}
	stateMessage := ""
	if enabled && state.Status == "pending" {
		stateMessage = message
	}
	name := a.Name
	if name == "" {
		name = id
	}
	name = safeText(ctx, m, record, name)
	return tasks.Row(key, "agent_rule_sync", name, "resource_id", id, "enabled", enabled, "schedule_text", msgRowSchedule, "run_hint", msgRowHint, "running", enabled && state.Status == "running", "state_message", stateMessage, "link", "/agents", history, nil), nil
}
func nativeCall(ctx context.Context, m *agents.Manager, id, method string, paths []string) (nativeTask, error) {
	var selected any
	if len(paths) > 0 {
		selected = jsonx.ListOf(paths)
	}
	answer, _, err := monitor.RuleUpdateCall(ctx, monitor.FromAgents(m), id, method, selected)
	if err != nil {
		return nativeTask{}, err
	}
	raw, err := json.Marshal(answer)
	if err != nil {
		return nativeTask{}, err
	}
	var fields model.Fields
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nativeTask{}, err
	}
	task, present := fields.Get("task")
	if !present {
		return nativeTask{}, nil
	}
	var result nativeTask
	if err = json.Unmarshal(task, &result); err != nil {
		return nativeTask{}, err
	}
	return result, nil
}
func refreshProvider(ctx context.Context, m *agents.Manager, id, name string) error {
	_, err := monitor.PanelCall(ctx, monitor.FromAgents(m), id, "providers/rule", "PUT", [][2]string{{"name", name}}, nil, "")
	return err
}
func resultStatus(r *agents.Result) string { return r.GetString("status") }
func beginPublication(ctx context.Context, m *agents.Manager, id, deploymentID string) (bool, error) {
	r, e := m.BeginDeployment(ctx, id, deploymentID)
	if e != nil {
		return false, e
	}
	return r.Value("success") == true, nil
}
func failUnsubmitted(ctx context.Context, m *agents.Manager, id, deploymentID string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	data, err := json.Marshal(struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}{false, "failed", msgCandidateFailed})
	if err != nil {
		return err
	}
	patch, err := agents.DecodeDeploymentUpdate(data)
	if err != nil {
		return err
	}
	_, err = m.RecordDeploymentModel(ctx, id, deploymentID, patch, agents.RecordOptions{OnlyUnsubmitted: true})
	return err
}
func pollDeployment(ctx context.Context, m *agents.Manager, id, deploymentID string) (*model.Deployment, error) {
	return m.GetDeploymentModel(ctx, id, deploymentID)
}

func isOxi(record *agents.Record) bool { return record.GetString("service_type") == "oxidns" }
func profileAgents(ctx context.Context, r *repo.Repository, profileID string) ([]*agents.Record, error) {
	defaultID, err := r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	if profileID == "" {
		profileID = defaultID
	}
	out := []*agents.Record{}
	err = r.ViewAgents(ctx, func(records []model.Agent) error {
		for i := range records {
			a := &records[i]
			if !agents.AgentMatchesID(a, a.ID) || !agents.AgentEnabledModel(a) {
				continue
			}
			raw, present, err := a.MemberJSON("profile_id")
			if err != nil {
				return err
			}
			var binding *string
			if present && json.Unmarshal(raw, &binding) != nil {
				continue
			}
			id := defaultID
			if binding != nil && *binding != "" {
				id = *binding
			}
			if id != profileID {
				continue
			}
			view := *a
			for _, field := range repo.AgentHistoryFields {
				if err := view.Clear(field); err != nil {
					return err
				}
			}
			data, err := model.Marshal(view)
			if err != nil {
				return err
			}
			var record agents.Record
			if err := json.Unmarshal(data, &record); err != nil {
				return err
			}
			out = append(out, &record)
		}
		return nil
	})
	return out, err
}
func workerAgent(ctx context.Context, m *agents.Manager, id string) (*agents.Record, error) {
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil || a == nil {
		return nil, err
	}
	profile, err := m.ResolveAgentProfile(ctx, a)
	if err != nil {
		return nil, err
	}
	if !agents.FreshHeartbeat(a.LastHeartbeat, nil) {
		a.Status = "offline"
	}
	a.Enabled = ptr(agents.AgentEnabledModel(a))
	a.ProfileID = profile
	data, err := model.Marshal(a)
	if err != nil {
		return nil, err
	}
	var record agents.Record
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}
func repositorySource(ctx context.Context, r *repo.Repository, profileID string) (*deploy.Source, error) {
	config, revision, err := r.PublishConfig(ctx, profileID, false)
	if err != nil {
		return nil, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, err
	}
	return deploy.SourceFromJSON(raw, &revision)
}

func snapshotVersion(record *agents.Record) string {
	applied := record.GetObject("applied_content")
	if applied != nil && applied.Has("config_version") {
		return applied.GetString("config_version")
	}
	return record.GetString("config_version")
}

// Presence uses the shared transition owner, with only its log-facing fields
// projected and sanitized. No metrics/history or deployment graph is copied.
func observePresence(ctx context.Context, m *agents.Manager, records []*agents.Record) {
	settings, err := m.Repository().GetSystemSettings(ctx)
	var secrets redact.Secrets
	if err == nil {
		secrets, err = redact.ExternalPayloadSecrets(ctx, settings)
	}
	for _, record := range records {
		secrets = redact.CollectCredentials(record, secrets)
	}
	safe := make([]*agents.Record, 0, len(records))
	for _, record := range records {
		name := record.Value("name")
		if !jsonx.Truthy(name) {
			name = record.Value("id")
		}
		fields := jsonx.Obj("name", name, "last_heartbeat", record.Value("last_heartbeat"))
		if err != nil {
			fields.Set("name", redact.Redacted)
			fields.Set("last_heartbeat", redact.Redacted)
		} else {
			for _, key := range []string{"name", "last_heartbeat"} {
				if value, ok := fields.Value(key).(string); ok {
					fields.Set(key, safeDisplay(value, secrets))
				} else {
					single := agents.SanitizePublicMetadata(jsonx.Obj("value", fields.Value(key)), secrets)
					if single == nil {
						fields.Set(key, redact.Redacted)
					} else {
						fields.Set(key, single.Value("value"))
					}
				}
			}
		}
		fields.Set("id", record.Value("id"))
		fields.Set("status", record.Value("status"))
		safe = append(safe, fields)
	}
	agents.ObservePresence(safe)
}

func safeDisplay(text string, secrets redact.Secrets) string {
	safe := redact.DiagnosticText(text, secrets, 2000)
	if redact.ContainsEncodedSecret(safe, secrets) {
		return redact.Redacted
	}
	return safe
}

func pairEnabling(record *agents.Record) bool {
	return agents.HAActive(record) && jsonx.Truthy(record.GetObject(agents.HAField).Value("enable"))
}

// UnmarshalJSON accepts exact protocol names without struct tag folding.
func (task *nativeTask) UnmarshalJSON(raw []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	var next nativeTask
	for _, field := range []struct {
		name   string
		target *string
	}{{"id", &next.ID}, {"status", &next.Status}, {"message", &next.Message}} {
		if value, present := fields.Get(field.name); present {
			if err := json.Unmarshal(value, field.target); err != nil {
				return err
			}
		}
	}
	*task = next
	return nil
}
