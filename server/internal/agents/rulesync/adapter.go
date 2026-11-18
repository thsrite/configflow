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
	raw, revision, err := m.Repository().GetPublishSnapshot(ctx, profileID, false)
	if err != nil {
		return nil, err
	}
	return deploy.NewSource(raw, &revision), nil
}

func persistSync(ctx context.Context, m *agents.Manager, id string, settings *Settings, state *model.RuleSyncState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := m.UpdateAgents(ctx, "", func(tx context.Context, list *[]any) (any, error) {
		if err := tx.Err(); err != nil {
			return nil, err
		}
		for _, entry := range *list {
			record, ok := entry.(*jsonx.Object)
			if !ok || record.GetString("id") != id {
				continue
			}
			if settings != nil && pairEnabling(record) {
				return nil, refuse(msgPairEnabling)
			}
			sync := record.GetObject("rule_sync")
			if sync == nil {
				sync = jsonx.NewObject()
			}
			if settings != nil {
				sync.Set("settings", jsonx.Obj("library", settings.Library, "rules", settings.Rules))
			}
			if state != nil {
				raw, err := encodeState(state)
				if err != nil {
					return nil, err
				}
				value, err := jsonx.Parse(raw)
				if err != nil {
					return nil, err
				}
				sync.Set("state", value)
			}
			record.Set("rule_sync", sync)
			return nil, tx.Err()
		}
		return nil, ErrAgentNotFound
	})
	return err
}

func renderCandidate(ctx context.Context, target deploy.Target, source *deploy.Source, baseURL string) (string, []convert.Download, error) {
	raw, err := source.Raw()
	if err != nil {
		return "", nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return "", nil, err
	}
	config, ok := value.(*jsonx.Object)
	if !ok {
		return "", nil, refuse(msgSnapshotVerify)
	}
	if target.ServiceType == "mihomo" {
		content, err := convert.GenerateMihomoConfig(ctx, config, convert.GenerateOptions{BaseURL: baseURL, SyncLANHosts: true, SkipProviderPreflight: true, ForAgent: true, RegionProbe: target.DomainDiscovery})
		return content, nil, err
	}
	p := oxidns.NewProfile(config)
	content, err := oxidns.Generate(p, baseURL)
	if err != nil {
		return "", nil, err
	}
	entries, err := oxidns.RulesetDownloads(p, baseURL)
	if err != nil {
		return "", nil, err
	}
	downloads := make([]convert.Download, 0, len(entries))
	for _, e := range entries {
		downloads = append(downloads, convert.Download{URL: e.URL, LocalPath: e.LocalPath})
	}
	return content, downloads, nil
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
	_, err := m.UpdateAgents(ctx, "", func(tx context.Context, entries *[]any) (any, error) {
		if e := tx.Err(); e != nil {
			return nil, e
		}
		byID := map[string]*jsonx.Object{}
		for _, entry := range *entries {
			if r, ok := entry.(*jsonx.Object); ok {
				byID[r.GetString("id")] = r
			}
		}
		for _, member := range members {
			current := byID[member.GetString("id")]
			if current == nil || !jsonx.Equal(current.Value("oxidns_ha"), member.Value("oxidns_ha")) {
				return nil, refuse(msgPairChanged)
			}
		}
		for _, member := range members {
			if pairEnabling(byID[member.GetString("id")]) {
				return nil, refuse(msgPairEnabling)
			}
		}
		for _, member := range members {
			current := byID[member.GetString("id")]
			sync := current.GetObject("rule_sync")
			if sync == nil {
				sync = jsonx.NewObject()
			}
			sync.Set("settings", jsonx.Obj("library", s.Library, "rules", s.Rules))
			current.Set("rule_sync", sync)
		}
		return nil, tx.Err()
	})
	return err
}
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
	var view struct {
		Task nativeTask `json:"task"`
	}
	if err = json.Unmarshal(raw, &view); err != nil {
		return nativeTask{}, err
	}
	return view.Task, nil
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
	_, e := m.RecordDeployment(ctx, id, deploymentID, jsonx.Obj("success", false, "status", "failed", "message", msgCandidateFailed), agents.RecordOptions{OnlyUnsubmitted: true})
	return e
}
func pollDeployment(ctx context.Context, m *agents.Manager, id, deploymentID string) (*model.Deployment, error) {
	r, e := m.GetDeployment(ctx, id, deploymentID)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(r)
	if e != nil {
		return nil, e
	}
	var out model.Deployment
	e = json.Unmarshal(raw, &out)
	return &out, e
}

func isOxi(record *agents.Record) bool { return record.GetString("service_type") == "oxidns" }
func profileAgents(ctx context.Context, r *repo.Repository, profileID string) ([]*agents.Record, error) {
	records, e := r.GetAgents(ctx, false)
	if e != nil {
		return nil, e
	}
	settings, e := r.GetSystemSettings(ctx)
	if e != nil {
		return nil, e
	}
	defaultID, e := r.DefaultProfileID(ctx)
	if e != nil {
		return nil, e
	}
	settings.Set("default_profile_id", defaultID)
	light := make([]any, 0, len(records))
	for _, entry := range records {
		if record, ok := entry.(*jsonx.Object); ok {
			light = append(light, agentView(record))
		}
	}
	raw, e := json.Marshal(jsonx.Obj("agents", light, "profile_id", profileID, "system_config", settings))
	if e != nil {
		return nil, e
	}
	source, e := oxidns.ProfileFromJSON(raw)
	if e != nil {
		return nil, e
	}
	selected := oxidns.ProfileAgents(source)
	out := make([]*agents.Record, 0, len(selected))
	for _, node := range selected {
		record, ok := oxidns.ToJSONX(node).(*jsonx.Object)
		if !ok {
			return nil, refuse(msgSnapshotVerify)
		}
		out = append(out, record)
	}
	return out, nil
}
func workerAgent(ctx context.Context, m *agents.Manager, id string) (*agents.Record, error) {
	record, e := m.GetAgentByID(ctx, id, false)
	if e != nil || record == nil {
		return record, e
	}
	record.Set("status", agents.OnlineStatus(record, nil))
	record.Set("enabled", jsonx.Truthy(record.GetOr("enabled", true)))
	if record.GetString("profile_id") == "" {
		profile, e := m.Repository().DefaultProfileID(ctx)
		if e != nil {
			return nil, e
		}
		record.Set("profile_id", profile)
	}
	return record, nil
}
func repositorySource(ctx context.Context, r *repo.Repository, profileID string) (*deploy.Source, error) {
	raw, revision, e := r.GetPublishSnapshot(ctx, profileID, false)
	if e != nil {
		return nil, e
	}
	return deploy.NewSource(raw, &revision), nil
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
	safe := agents.FailureText(text, jsonx.ListOf([]string(secrets)), 2000)
	if redact.ContainsEncodedSecret(safe, secrets) {
		return redact.Redacted
	}
	return safe
}

func pairEnabling(record *agents.Record) bool {
	return agents.HAActive(record) && jsonx.Truthy(record.GetObject(agents.HAField).Value("enable"))
}
