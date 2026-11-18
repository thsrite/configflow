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
	"strings"

	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/oxidns"
)

func decodeAgent(record *agents.Record) (*model.Agent, error) {
	raw, err := json.Marshal(record)
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
	a, err := decodeAgent(record)
	if err != nil || a.RuleSync == nil {
		return Settings{}
	}
	s := a.RuleSync.Settings
	return Settings{Library: s.Library != nil && *s.Library, Rules: s.Rules != nil && *s.Rules}
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
	system, err := m.Repository().GetSystem(ctx)
	if err != nil {
		return "[REDACTED]"
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, system.GetObject("system_config"))
	if err != nil {
		return "[REDACTED]"
	}
	secrets = redact.CollectCredentials(record, secrets)
	safe := redact.SanitizeCredentials(jsonx.Obj("message", text), secrets)
	return agents.FailureText(safe.GetString("message"), []any{record.Value("token")}, 2000)
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
	history := tasks.History(statuses.GetObject(key))
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
		selected = paths
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
	_, err := monitor.PanelCall(ctx, monitor.FromAgents(m), id, "providers/rule", "PUT", nil, jsonx.Obj("name", name), "")
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
	system, e := r.GetSystem(ctx)
	if e != nil {
		return nil, e
	}
	defaultID, e := r.DefaultProfileID(ctx)
	if e != nil {
		return nil, e
	}
	out := []*agents.Record{}
	for _, entry := range system.GetList("agents") {
		record, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		profile := record.GetString("profile_id")
		if profile == "" || profile == "default" {
			profile = defaultID
		}
		if profile == profileID {
			out = append(out, record)
		}
	}
	return out, nil
}
func repositorySource(ctx context.Context, r *repo.Repository, profileID string) (*deploy.Source, error) {
	raw, revision, e := r.GetPublishSnapshot(ctx, profileID, false)
	if e != nil {
		return nil, e
	}
	return deploy.NewSource(raw, &revision), nil
}
