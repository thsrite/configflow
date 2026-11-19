package agents

// Port of backend/agents/manager.py (AgentManager): registration,
// re-registration with the existing token, heartbeats and the in-memory
// runtime state, Agent records in the system document, profile binding and
// the HTTP calls to Agents. The deployment bookkeeping is in deployments.go.

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.agents.manager")

// HeartbeatFreshSeconds is HEARTBEAT_FRESH_SECONDS: an Agent heartbeats every
// 30–60 seconds; one silent for longer than this is offline.
const HeartbeatFreshSeconds = 120

// OxidnsHALocked is OXIDNS_HA_LOCKED.
const OxidnsHALocked = "DNS 高可用已开启，请先停用 DNS 高可用"

// DeploymentConflict is AgentDeploymentConflict (a ValueError): a binding or
// registration cannot change during an unfinished operation (409).
type DeploymentConflict struct{ Message string }

func (e *DeploymentConflict) Error() string { return e.Message }

// PermissionError is the PermissionError('Unauthorized') of a re-registration
// without the existing Agent's token (401).
type PermissionError struct{}

func (e *PermissionError) Error() string { return "Unauthorized" }

// ValueError is a Python ValueError raised by the registry (str(e) is Message).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

var errNothingToSave = errors.New("nothing to save")

// pyStatuses is a tuple of status strings tested with Python's `in`: a value
// of any other type (a list or dict an Agent answered included) is simply not
// a member, it never panics like an `any`-keyed map lookup would.
type pyStatuses map[string]bool

func (s pyStatuses) has(value any) bool {
	text, ok := value.(string)
	return ok && s[text]
}

var deploymentTerminal = pyStatuses{"succeeded": true, "failed": true, "rolled_back": true}

// deploymentResults is _DEPLOYMENT_RESULTS.
var deploymentResults = map[string]struct {
	level logx.Level
	text  string
}{
	"succeeded":       {logx.Info, "配置发布成功"},
	"failed":          {logx.Warning, "配置发布失败"},
	"rolled_back":     {logx.Warning, "配置发布未生效，已回滚到原配置"},
	"rollback_failed": {logx.Error, "配置发布失败，且回滚失败"},
}

// agentLabel is _agent_label(agent).
func agentLabel(agent *jsonx.Object) string {
	name := agent.Value("name")
	if !jsonx.Truthy(name) {
		name = agent.Value("id")
	}
	return logx.LogText(name, 120)
}

// ConstantTimeASCIIEqual is _constant_time_ascii_equal (manager.py and
// routes/agents.py): hmac.compare_digest of the ASCII bytes; non-strings and
// non-ASCII text are simply unequal.
func ConstantTimeASCIIEqual(provided, expected any) bool {
	p, ok1 := provided.(string)
	e, ok2 := expected.(string)
	if !ok1 || !ok2 || !isASCII(p) || !isASCII(e) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(p), []byte(e)) == 1
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// HeartbeatFresh is heartbeat_fresh(agent, now): the last heartbeat is at
// most two minutes old. now nil is the current time; a given now is Python's
// naive datetime (its wall clock in the local zone), the only kind callers
// pass. Unparsable or missing heartbeats are stale.
//
// As in Python, a naive heartbeat minus a naive now is the difference of the
// two wall clocks (a DST change in between is not seen), an aware heartbeat
// is compared with the current instant, and an aware heartbeat against a
// given (naive) now is a TypeError, i.e. stale. Both sides have microsecond
// precision, like datetime.
func HeartbeatFresh(agent *jsonx.Object, now *time.Time) bool {
	return FreshHeartbeat(agent.GetString("last_heartbeat"), now)
}

// FreshHeartbeat applies the registry freshness rule to a typed timestamp.
func FreshHeartbeat(text string, now *time.Time) bool {
	heartbeat, naive, ok := fromISO(text)
	if !ok {
		return false
	}
	var elapsed int64 // microseconds
	if naive {
		current := clock()
		if now != nil {
			current = *now
		}
		wall, _ := taskstatus.FromISOFormat(text) // the naive fields, read as UTC
		elapsed = wallMicros(current.In(localZone)) - wallMicros(wall)
	} else {
		if now != nil {
			return false // can't subtract offset-naive and offset-aware datetimes
		}
		elapsed = unixMicros(clock()) - unixMicros(heartbeat)
	}
	return elapsed <= HeartbeatFreshSeconds*1_000_000
}

// wallMicros is the wall clock of t (its own fields, whatever the zone) in
// microseconds, as naive datetime arithmetic sees it.
func wallMicros(t time.Time) int64 {
	return unixMicros(time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC))
}

func unixMicros(t time.Time) int64 { return t.Unix()*1_000_000 + int64(t.Nanosecond()/1000) }

// OnlineStatus is online_status(agent, now): the stored status, or
// "offline" once the heartbeat is stale.
func OnlineStatus(agent *jsonx.Object, now *time.Time) any {
	if HeartbeatFresh(agent, now) {
		return agent.Value("status")
	}
	return "offline"
}

// IsOnline is is_online(agent, now).
func IsOnline(agent *jsonx.Object, now *time.Time) bool { return OnlineStatus(agent, now) == "online" }

// IsUsable is is_usable(agent, now): online by the freshness rule and not
// disabled.
func IsUsable(agent *jsonx.Object, now *time.Time) bool {
	return agent.Value("enabled") != false && IsOnline(agent, now)
}

// upgradeBusy is _upgrade_busy(agent).
func upgradeBusy(agent *jsonx.Object) bool {
	upgrade := agent.Value("latest_upgrade")
	if !jsonx.Truthy(upgrade) {
		return false
	}
	object, _ := upgrade.(*jsonx.Object)
	return !deploymentTerminal.has(object.Value("status"))
}

// haEnabled is _oxidns_ha_enabled(agent).
func haEnabled(agent *jsonx.Object) bool {
	ha, _ := agent.Value("oxidns_ha").(*jsonx.Object)
	return ha.Value("enabled") == true
}

// haBusy is _oxidns_ha_busy(agent).
func haBusy(agent *jsonx.Object) bool { return haActive(agent) }

// dnsFollowBusy is _dns_follow_busy(agents, agent_id).
func dnsFollowBusy(agents []any, agentID any) bool {
	for _, item := range agents {
		agent, _ := item.(*jsonx.Object)
		state, _ := agent.Value("dns_follow_state").(*jsonx.Object)
		status := state.Value("status")
		if status != "preparing" && status != "publishing" {
			continue
		}
		follow, _ := agent.Value("dns_follow").(*jsonx.Object)
		if jsonx.Equal(agent.Value("id"), agentID) || jsonx.Equal(follow.Value("source_agent_id"), agentID) {
			return true
		}
	}
	return false
}

// unfinishedDeployment reports a deployment whose status is not terminal.
func unfinishedDeployment(agent *jsonx.Object) bool {
	deployments, _ := agent.Value("deployments").(*jsonx.Object)
	for _, task := range deployments.Values() {
		object, _ := task.(*jsonx.Object)
		if !deploymentTerminal.has(object.Value("status")) {
			return true
		}
	}
	return false
}

// truthyOr is `value or fallback`.
func truthyOr(value, fallback any) any {
	if jsonx.Truthy(value) {
		return value
	}
	return fallback
}

func objectAt(agents []any, i int) *jsonx.Object {
	object, _ := agents[i].(*jsonx.Object)
	return object
}

func findAgent(agents []any, agentID any) (*jsonx.Object, int) {
	for i, item := range agents {
		if agent, ok := item.(*jsonx.Object); ok && jsonx.Equal(agent.Value("id"), agentID) {
			return agent, i
		}
	}
	return nil, -1
}

// Manager is AgentManager: the registry of one repository.
type Manager struct {
	closeMu    sync.Mutex
	closed     bool
	closeHooks []func()

	repository *repo.Repository
	runtime    *RuntimeState
	client     Client

	deploymentMu sync.Mutex
	preparations map[[2]string]*preparationLease

	// Optional test seams. A nil sleep uses the Runtime Clock and honours ctx.
	sleep              func(time.Duration)
	monotonic          func() float64
	preparationRunning func(agentID, deploymentID string) (bool, error)
}

// New is AgentManager(repository): it shares the runtime state of the data
// directory and imports a legacy runtime.sqlite3 once.
func New(ctx context.Context, repository *repo.Repository) (*Manager, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m := &Manager{
		repository:   repository,
		runtime:      RuntimeFor(repository.DataDir()),
		preparations: map[[2]string]*preparationLease{},
		monotonic:    func() float64 { return time.Since(processStart).Seconds() },
	}
	m.preparationRunning = m.defaultPreparationRunning
	if err := importLegacyDatabase(ctx, repository, m.runtime, m.saveReportedModels); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

// Repository is the repository the manager works on.
func (m *Manager) Repository() *repo.Repository { return m.repository }

// Runtime is the in-memory heartbeat state.
func (m *Manager) Runtime() *RuntimeState { return m.runtime }

// SetClient makes this manager use client instead of DefaultClient().
func (m *Manager) SetClient(client Client) { m.client = client }

// Client is the client this manager sends Agent requests with (its own or
// DefaultClient()); for packages that build their own Agent URLs
// (oxidns_monitor's validated origin).
func (m *Manager) Client() Client { return m.httpClient() }

func (m *Manager) httpClient() Client {
	if m.client != nil {
		return m.client
	}
	return DefaultClient()
}

func (m *Manager) defaultProfileID(ctx context.Context) (string, error) {
	return m.repository.DefaultProfileID(ctx)
}

// profileOf is (agent.get('profile_id') or default_profile_id()).
func (m *Manager) profileOf(ctx context.Context, agent *jsonx.Object) (any, error) {
	if value := agent.Value("profile_id"); jsonx.Truthy(value) {
		return value, nil
	}
	id, err := m.defaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	return id, nil
}

// agents is _agents(history=...): the records with the heartbeat state laid
// over them.
func (m *Manager) agents(ctx context.Context, history bool) ([]*jsonx.Object, error) {
	return m.agentsInspect(ctx, history, nil)
}

func (m *Manager) agentsInspect(ctx context.Context, history bool, inspect func(*jsonx.Object)) ([]*jsonx.Object, error) {
	var inspectErr error
	var inspectModel func(*model.Agent)
	if inspect != nil {
		inspectModel = func(record *model.Agent) {
			if inspectErr != nil {
				return
			}
			raw, err := agentObject(record)
			if err != nil {
				inspectErr = err
				return
			}
			inspect(raw)
		}
	}
	records, err := m.ReadAgents(ctx, history, inspectModel)
	if err != nil {
		return nil, err
	}
	if inspectErr != nil {
		return nil, inspectErr
	}
	result := make([]*jsonx.Object, 0, len(records))
	for i := range records {
		raw, err := agentObject(&records[i])
		if err != nil {
			return nil, err
		}
		if !history {
			for _, key := range repo.AgentHistoryFields {
				raw.Set(key, repo.AgentHistoryNotRead)
			}
		}
		result = append(result, raw)
	}
	return result, nil
}

// Agents is _agents(history=...): every record with its heartbeat state,
// keeping the stored status.
func (m *Manager) Agents(ctx context.Context, history bool) ([]*jsonx.Object, error) {
	return m.agents(ctx, history)
}

// saveReportedFields is _save_reported_fields: write {agent_id: {field:
// value}} in one transaction; returns the IDs written (nothing when none of
// the Agents exists any more).
func (m *Manager) saveReportedFields(ctx context.Context, changes map[string]*jsonx.Object) (map[string]bool, error) {
	changesTyped := make(map[string]model.Fields, len(changes))
	for id, record := range changes {
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		var fields model.Fields
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		changesTyped[id] = fields
	}
	return m.saveReportedModels(ctx, changesTyped)
}

// UpdateAgents is _update_agents(updater, profile_id): the updater edits the
// Agent list of the system section inside one repository transaction (append
// or replace through the pointer). profileID, when given, must still exist
// at commit. ProfileInUse becomes *DeploymentConflict.
func (m *Manager) UpdateAgents(ctx context.Context, profileID string, updater func(ctx context.Context, agents *[]any) (any, error)) (any, error) {
	return m.updateRegistry(ctx, profileID, func(ctx context.Context, list *[]any, _ *model.System) (any, error) { return updater(ctx, list) })
}

func (m *Manager) updateRegistry(ctx context.Context, profileID string, updater func(context.Context, *[]any, *model.System) (any, error)) (any, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if testFailUpdate != nil {
		if err := testFailUpdate(); err != nil {
			return nil, err
		}
	}
	var result any
	err := m.repository.UpdateSystem(ctx, func(ctx context.Context, system *model.System) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if profileID != "" {
			if _, err := m.repository.ProfileMetadata(ctx, profileID); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(system.Agents)
		if err != nil {
			return err
		}
		value, err := jsonx.Parse(raw)
		if err != nil {
			return err
		}
		list, _ := value.([]any)
		result, err = updater(ctx, &list, system)
		if err != nil {
			return err
		}
		raw, err = jsonx.Compact(list)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return system.SetJSON("agents", raw)
	})
	if err != nil {
		if errors.Is(err, repo.ErrInUse) {
			return nil, &DeploymentConflict{Message: err.Error()}
		}
		return nil, err
	}
	return result, nil
}

// validateProfileID is ProfileRepository.validate_profile_id for any value.
func validateProfileID(value any) error {
	text, ok := value.(string)
	if !ok {
		return repo.NewValidationError("Invalid profile id")
	}
	return repo.ValidateProfileID(text)
}

func objOr(value any) *jsonx.Object {
	object, _ := value.(*jsonx.Object)
	return object
}

// GetAllAgents is get_all_agents(history=...): every Agent with its status
// derived by the freshness rule (offline after two minutes without a
// heartbeat). history=false leaves the publication history unread.
func (m *Manager) GetAllAgents(ctx context.Context, history bool) ([]*jsonx.Object, error) {
	return m.GetAllAgentsInspect(ctx, history, nil)
}

// GetAllAgentsInspect inspects each raw record before history projection,
// preserving GetAllAgents' runtime overlay and online-status calculation.
// inspect must not mutate or retain records; N8-5 must keep this capability.
func (m *Manager) GetAllAgentsInspect(ctx context.Context, history bool, inspect func(*jsonx.Object)) ([]*jsonx.Object, error) {
	agents, err := m.agentsInspect(ctx, history, inspect)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		agent.Set("status", OnlineStatus(agent, nil))
	}
	return agents, nil
}

// GetAgentByID is get_agent_by_id(agent_id, history=...): the record with
// its stored status (never derived offline), nil when unknown.
func (m *Manager) GetAgentByID(ctx context.Context, agentID string, history bool) (*jsonx.Object, error) {
	record, err := m.ReadAgent(ctx, agentID, history)
	if err != nil || record == nil {
		return nil, err
	}
	raw, err := agentObject(record)
	if err == nil && !history {
		for _, key := range repo.AgentHistoryFields {
			raw.Set(key, repo.AgentHistoryNotRead)
		}
	}
	return raw, err
}

// GetAgentByToken is get_agent_by_token(token).
func (m *Manager) GetAgentByToken(ctx context.Context, token string) (*jsonx.Object, error) {
	record, err := m.ReadAgentByToken(ctx, token)
	if err != nil || record == nil {
		return nil, err
	}
	return agentObject(record)
}

var editableFields = []string{"name", "host", "port", "enabled", "service_type", "profile_id"}
var guardedFields = []string{"host", "port", "enabled", "service_type", "profile_id"}

// UpdateAgent is update_agent(agent_id, updates): the editable fields only;
// false when the Agent does not exist.
func (m *Manager) UpdateAgent(ctx context.Context, agentID string, updates *jsonx.Object) (bool, error) {
	data, err := json.Marshal(updates)
	if err != nil {
		return false, err
	}
	edits, err := DecodeAgentEdits(data)
	if err != nil {
		return false, err
	}
	return m.EditAgent(ctx, agentID, edits)
}

// RebindProfileAgents is rebind_profile_agents(source, target,
// expected_agent_ids): move exactly the reviewed bindings together, under
// the deployment transaction lock.
func (m *Manager) RebindProfileAgents(ctx context.Context, sourceProfileID, targetProfileID, expectedAgentIDs any) (*jsonx.Object, error) {
	if err := validateProfileID(sourceProfileID); err != nil {
		return nil, err
	}
	if err := validateProfileID(targetProfileID); err != nil {
		return nil, err
	}
	source, target := sourceProfileID.(string), targetProfileID.(string)
	if source == target {
		return nil, repo.NewValidationError(msgRebindOtherProfile)
	}
	items, valid := expectedAgentIDs.([]any)
	if !valid {
		return nil, repo.NewValidationError(msgRebindReviewedIDs)
	}
	ids := make([]string, len(items))
	for i, item := range items {
		id, valid := item.(string)
		if !valid {
			return nil, repo.NewValidationError(msgRebindReviewedIDs)
		}
		ids[i] = id
	}
	result, err := m.RebindProfiles(ctx, source, target, ids)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var response Result
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// UpdateHeartbeat is update_heartbeat(agent_id, heartbeat_data): the state is
// kept in memory only; version and config_version are written to the
// configuration only when they change. false when the Agent is unknown (or
// was deleted meanwhile).
func (m *Manager) UpdateHeartbeat(ctx context.Context, agentID string, heartbeat *jsonx.Object) (bool, error) {
	payload, err := heartbeatFromObject(heartbeat)
	if err != nil {
		return false, err
	}
	return m.observeHeartbeat(ctx, agentID, payload, nil)
}

// heartbeatFromObject reads the reported members by their exact names (Python
// heartbeat.get(key)); encoding/json struct decoding would fold case. Each
// value is encoded on its own, giving the bytes the whole-object encoding has.
func heartbeatFromObject(heartbeat *jsonx.Object) (heartbeatPayload, error) {
	var payload heartbeatPayload
	if heartbeat == nil {
		return payload, nil
	}
	for _, item := range []struct {
		name   string
		target *json.RawMessage
	}{
		{"version", &payload.Version}, {"config_version", &payload.ConfigVersion},
		{"service_status", &payload.ServiceStatus}, {"system_metrics", &payload.SystemMetrics},
	} {
		value, present := heartbeat.Get(item.name)
		if !present {
			continue
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return payload, fmt.Errorf("encode heartbeat %s: %w", item.name, err)
		}
		*item.target = raw
	}
	return payload, nil
}

// testFailUpdate makes UpdateAgents fail (tests; Python patched
// repository.update_system_transaction).
var testFailUpdate func() error

var testRecordHeartbeat func(m *Manager, agent, payload *jsonx.Object, mark int64) bool

// SetDomainDiscovery is set_domain_discovery(agent_id, enabled): only mihomo
// Agents can enable it. Returns the updated live record, nil when unknown.
func (m *Manager) SetDomainDiscovery(ctx context.Context, agentID string, enabled bool) (*jsonx.Object, error) {
	record, err := m.SetDomainDiscoveryModel(ctx, agentID, enabled)
	if err != nil || record == nil {
		return nil, err
	}
	return agentObject(record)
}

// SetTrafficHistory changes the management switch through the native registry.
func (m *Manager) SetTrafficHistory(ctx context.Context, agentID string, enabled bool) (*jsonx.Object, error) {
	record, err := m.SetTrafficHistoryModel(ctx, agentID, enabled)
	if err != nil || record == nil {
		return nil, err
	}
	return agentObject(record)
}
