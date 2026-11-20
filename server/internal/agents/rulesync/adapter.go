package rulesync

// Registry, task and deployment collaborators use owned native records.
// Reconciliation consumes immutable deploy.Source values and yamlx nodes.
import (
	"configflow/internal/agents/monitor"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"context"
	"encoding/json"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
)

// AgentSettings reads the typed per-Agent scopes. Malformed stored switches are off.
func AgentSettings(record *model.Agent) Settings {
	if record == nil || record.RuleSync == nil {
		return Settings{}
	}
	s := record.RuleSync.Settings
	return Settings{Library: s.Library != nil && *s.Library, Rules: s.Rules != nil && *s.Rules}
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

func recordID(r *model.Agent) string {
	if r == nil {
		return ""
	}
	return r.ID
}
func recordSupported(r *model.Agent) bool {
	return r != nil && (r.ServiceType == "mihomo" || r.ServiceType == "oxidns")
}
func compareLeader(a, b *model.Agent) int {
	ap := a.OxiDNSHA != nil && a.OxiDNSHA.Role == "primary"
	bp := b.OxiDNSHA != nil && b.OxiDNSHA.Role == "primary"
	if ap != bp {
		if ap {
			return -1
		}
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}
func configurePair(ctx context.Context, m *agents.Manager, members []*model.Agent, s Settings) error {
	expected := make([]*model.Agent, 0, len(members))
	for _, member := range members {
		a := member
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

func systemSecrets(ctx context.Context, m *agents.Manager) (redact.Secrets, error) {
	system, err := m.Repository().System(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := model.Marshal(&system.SystemConfig)
	if err != nil {
		return nil, err
	}
	return redact.ExternalPayloadSecrets(ctx, json.RawMessage(raw))
}
func safeText(ctx context.Context, m *agents.Manager, record *model.Agent, text string) string {
	secrets, err := systemSecrets(ctx, m)
	if err != nil {
		return redact.Redacted
	}

	data, encodeErr := model.Marshal(record)
	if encodeErr != nil {
		return "[REDACTED]"
	}
	secrets = redact.CollectCredentials(json.RawMessage(data), secrets)
	return safeDisplay(text, secrets)
}
func logState(ctx context.Context, m *agents.Manager, record *model.Agent, status, message string) {
	name, _ := agents.AgentName(record)
	if name == "" {
		name = record.ID
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
func agentTaskRow(ctx context.Context, a *model.Agent, statuses taskstatus.Snapshot) (*tasks.Task, error) {
	if !recordSupported(a) {
		return nil, nil
	}
	id := a.ID
	key := "agent_rule_sync:" + id
	enabled := AgentSettings(a).Any()
	_, recorded := statuses[key]
	if !enabled && !recorded {
		return nil, nil
	}
	state := stateOf(a)
	history, err := tasks.ProjectHistory(statuses.Entry(key))
	if err != nil {
		return nil, err
	}
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	message := safeText(ctx, m, a, state.Message)
	if enabled && state.Status == "blocked" {
		last := "skipped"
		history.LastStatus, history.LastError = &last, &message
	} else if enabled && (state.Status == "succeeded" || state.Status == "failed") {
		last, detail := "success", ""
		if state.Status == "failed" {
			last, detail = "failed", message
		}
		history.LastStatus, history.LastError = &last, &detail
	}
	stateMessage := ""
	if enabled && state.Status == "pending" {
		stateMessage = message
	}
	name, _ := agents.AgentName(a)
	if name == "" {
		name = id
	}
	name = safeText(ctx, m, a, name)
	link := "/agents"
	return &tasks.Task{Key: key, Category: "agent_rule_sync", Name: name, ResourceID: id, Enabled: enabled, ScheduleText: msgRowSchedule, RunHint: msgRowHint, Running: enabled && state.Status == "running", StateMessage: stateMessage, Link: &link, HistoryData: history}, nil
}
func nativeCall(ctx context.Context, m *agents.Manager, id, method string, paths []string) (nativeTask, error) {
	answer, _, err := monitor.CallRuleUpdate(ctx, monitor.TypedFromAgents(m), id, method, paths)
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
	_, err := monitor.CallPanel(ctx, monitor.TypedFromAgents(m), id, "providers/rule", "PUT", [][2]string{{"name", name}}, nil, "")
	return err
}
func resultStatus(r *model.Deployment) string { return r.Status }
func beginPublication(ctx context.Context, m *agents.Manager, id, deploymentID string) (bool, error) {
	r, e := m.ReserveDeployment(ctx, id, deploymentID)
	if e != nil {
		return false, e
	}
	return r != nil && r.Success, nil
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

func isOxi(record *model.Agent) bool { return record != nil && record.ServiceType == "oxidns" }
func profileAgents(ctx context.Context, r *repo.Repository, profileID string) ([]*model.Agent, error) {
	defaultID, err := r.DefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	if profileID == "" {
		profileID = defaultID
	}
	out := []*model.Agent{}
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
			var record model.Agent
			if err := json.Unmarshal(data, &record); err != nil {
				return err
			}
			out = append(out, &record)
		}
		return nil
	})
	return out, err
}
func workerAgent(ctx context.Context, m *agents.Manager, id string) (*model.Agent, error) {
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
	return a, nil
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

func snapshotVersion(a *model.Agent) string {
	if a.AppliedContent != nil {
		data, err := model.Marshal(a.AppliedContent)
		var fields model.Fields
		if err == nil {
			err = json.Unmarshal(data, &fields)
		}
		raw, present := fields.Get("config_version")
		if err == nil && present {
			var version string
			_ = json.Unmarshal(raw, &version)
			return version
		}
	}
	return a.ConfigVersion
}

// Presence uses the shared transition owner, with only its log-facing fields
// projected and sanitized. No metrics/history or deployment graph is copied.
func observePresence(ctx context.Context, m *agents.Manager, records []*model.Agent) {
	secrets, err := systemSecrets(ctx, m)
	for _, a := range records {
		data, failure := model.Marshal(a)
		if failure != nil {
			err = failure
		} else {
			secrets = redact.CollectCredentials(json.RawMessage(data), secrets)
		}
	}
	if err != nil {
		hidden := make([]agents.PresenceObservation, 0, len(records))
		for _, a := range records {
			if !agents.AgentMatchesID(a, a.ID) {
				continue
			}
			hidden = append(hidden, agents.PresenceObservation{ID: a.ID, Status: a.Status, Name: redact.Redacted, LastHeartbeat: redact.Redacted})
		}
		agents.ObserveAgentPresence(hidden, nil)
		return
	}
	agents.ObserveRegistryPresence(records, secrets)
}

func safeDisplay(text string, secrets redact.Secrets) string {
	safe := redact.DiagnosticText(text, secrets, 2000)
	if redact.ContainsEncodedSecret(safe, secrets) {
		return redact.Redacted
	}
	return safe
}

func pairEnabling(record *model.Agent) bool { return agents.HAEnablingModel(record) }

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

func receiptStamp(store *deploy.Receipts, a *model.Agent) (deploy.Stamp, bool) { return store.Stamp(a) }
func advanceReceiptContent(ctx context.Context, store *deploy.Receipts, m *agents.Manager, a *model.Agent, source *deploy.Source, bundle *deploy.Bundle) (bool, error) {
	return store.AdvanceContent(ctx, m, a, source, bundle)
}
