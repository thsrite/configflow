package agents

// Port of backend/agents/manager.py (AgentManager): registration,
// re-registration with the existing token, heartbeats and the in-memory
// runtime state, Agent records in the system document, profile binding and
// the HTTP calls to Agents. The deployment bookkeeping is in deployments.go.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
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

// unhashable is the TypeError of `value in some_dict` for a list or dict.
func unhashable(value any) error {
	switch value.(type) {
	case []any:
		return errors.New("unhashable type: 'list'")
	case *jsonx.Object:
		return errors.New("unhashable type: 'dict'")
	}
	return nil
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
	m := &Manager{
		repository:   repository,
		runtime:      RuntimeFor(repository.DataDir()),
		preparations: map[[2]string]*preparationLease{},
		monotonic:    func() float64 { return time.Since(processStart).Seconds() },
	}
	m.preparationRunning = m.defaultPreparationRunning
	if err := ImportLegacyDatabase(ctx, repository, m.runtime, m.saveReportedFields); err != nil {
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
	saved := map[string]bool{}
	_, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		for _, item := range *agents {
			agent, _ := item.(*jsonx.Object)
			id, _ := agent.Value("id").(string)
			if change, ok := changes[id]; ok {
				agent.Update(change)
				saved[id] = true
			}
		}
		if len(saved) == 0 {
			return nil, errNothingToSave
		}
		return nil, nil
	})
	if errors.Is(err, errNothingToSave) {
		return saved, nil
	}
	if err != nil {
		return map[string]bool{}, err
	}
	return saved, nil
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

func tokenHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func tokenURLSafe(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func objOr(value any) *jsonx.Object {
	object, _ := value.(*jsonx.Object)
	return object
}

// RegisterAgent is register_agent(agent_data, existing_token,
// installation_id): an Agent with the same name and host is updated only
// with its current token (keeping ID and token); otherwise a new record is
// created and its token returned once. existingToken and installationID are
// nil when absent.
func (m *Manager) RegisterAgent(ctx context.Context, data *jsonx.Object, existingToken, installationID any) (*jsonx.Object, error) {
	return m.registerAgent(ctx, data, existingToken, installationID, false, nil)
}

// RegisterInstallationAgent permits credential replay only for the exact trusted
// capability marker and unchanged installation identity, checked inside L3.
func (m *Manager) RegisterInstallationAgent(ctx context.Context, data *Record, existingToken, installationID string) (*Result, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.registerAgent(ctx, data, existingToken, installationID, true, nil)
}

func (m *Manager) registerAgent(ctx context.Context, data *jsonx.Object, existingToken, installationID any, replay bool, admission *RegistrationAuthorization) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := data.GetOr("name", "Unnamed Agent")
	host := data.GetOr("host", "")
	requested := data.Value("profile_id")
	profileArg := ""
	if jsonx.Truthy(requested) {
		if err := validateProfileID(requested); err != nil {
			return nil, err
		}
		profileArg = requested.(string)
	}
	serviceType := data.GetOr("service_type", "mihomo")
	port := data.GetOr("port", jsonx.Int(8080))
	result, err := m.updateRegistry(ctx, profileArg, func(ctx context.Context, agents *[]any, system *model.System) (any, error) {
		list := *agents
		existingIndex := -1
		for i, item := range list {
			agent, _ := item.(*jsonx.Object)
			if jsonx.Equal(agent.Value("name"), name) && jsonx.Equal(agent.Value("host"), host) {
				existingIndex = i
				break
			}
		}
		if existingIndex >= 0 {
			existing := objectAt(list, existingIndex)
			owned := replay && installationID != "" && jsonx.Equal(existing.Value("gateway_installation_id"), installationID)
			if owned {
				for _, key := range []string{"name", "host", "port", "service_type", "deployment_method", "profile_id"} {
					if !jsonx.Equal(existing.Value(key), data.Value(key)) {
						return nil, &DeploymentConflict{Message: installationIdentityChanged}
					}
				}
				existingToken = existing.Value("token")
			}
			if !ConstantTimeASCIIEqual(existingToken, existing.Value("token")) {
				// Without any first-registration proof the caller must get the same
				// refusal as for an unknown identity, or the public route becomes an
				// oracle for which name/host pairs are registered.
				if admission != nil && !admission.AllowNew && !m.proofMatches(ctx, system, data, admission) {
					return nil, ErrRegistrationAuthorization
				}
				return nil, &PermissionError{}
			}
			// An authenticated re-registration must not be blocked by stale bootstrap
			// data, but an accompanying still-valid one-use proof is spent on success.
			if admission != nil && admission.Credential != "" {
				optional := *admission
				optional.AllowNew = true
				if err := m.admitRegistration(ctx, system, data.DeepCopy(), &optional); err != nil {
					return nil, err
				}
			}
			if jsonx.Truthy(installationID) && jsonx.Equal(existing.Value("gateway_installation_id"), installationID) &&
				!jsonx.Equal(requested, existing.Value("profile_id")) {
				return nil, &DeploymentConflict{Message: "Installation Agent profile changed; prepare a new installation"}
			}
			if jsonx.Truthy(requested) {
				current, err := m.profileOf(ctx, existing)
				if err != nil {
					return nil, err
				}
				if !jsonx.Equal(requested, current) && (upgradeBusy(existing) || unfinishedDeployment(existing)) {
					return nil, &DeploymentConflict{Message: "Agent has an unfinished deployment or upgrade; recover it before changing profile"}
				}
			}
			changes := !jsonx.Equal(serviceType, existing.Value("service_type")) ||
				(jsonx.Truthy(requested) && !jsonx.Equal(requested, existing.Value("profile_id"))) ||
				!jsonx.Equal(port, existing.Value("port"))
			if dnsFollowBusy(list, existing.Value("id")) && changes {
				return nil, &DeploymentConflict{Message: "DNS 联动发布正在执行，请等待结果后再改变 Agent 注册信息"}
			}
			if (haEnabled(existing) || haBusy(existing)) && changes {
				return nil, &DeploymentConflict{Message: OxidnsHALocked}
			}
			agentID := existing.Value("id")
			profile := requested
			if !jsonx.Truthy(profile) {
				var err error
				if profile, err = m.profileOf(ctx, existing); err != nil {
					return nil, err
				}
			}
			updated := jsonx.Obj(
				"id", agentID,
				"name", name,
				"host", host,
				"port", port,
				"token", existing.Value("token"),
				"service_type", serviceType,
				"profile_id", profile,
				"deployment_method", data.GetOr("deployment_method", existing.GetOr("deployment_method", "unknown")),
				"status", "online",
				"last_heartbeat", nowISO(),
				"version", data.GetOr("version", "1.0.0"),
				"config_version", existing.GetOr("config_version", "0"),
				"enabled", true,
				"created_at", existing.GetOr("created_at", nowISO()),
				"updated_at", nowISO(),
				"deployments", existing.GetOr("deployments", jsonx.NewObject()),
				"latest_deployment", existing.Value("latest_deployment"),
				"latest_upgrade", existing.Value("latest_upgrade"),
				// 管理端设置的开关，重新注册不能把它清掉
				"domain_discovery_enabled", existing.GetOr("domain_discovery_enabled", false),
			)
			for _, field := range []string{"traffic_history_enabled", "traffic_history_since"} {
				if value, ok := existing.Get(field); ok {
					updated.Set(field, value)
				}
			}
			for _, field := range []string{"config_revision", "pushed_at", "applied_content", "dns_follow", "dns_follow_state", "dns_follow_job", "rule_sync",
				"oxidns_ha", "oxidns_ha_state", "oxidns_ha_publication", "ipv6_verification_settings", "ipv6_verifications"} {
				if value, ok := existing.Get(field); ok {
					updated.Set(field, jsonx.DeepCopy(value))
				}
			}
			if jsonx.Truthy(existing.Value("gateway_installation_id")) {
				updated.Set("gateway_installation_id", existing.Value("gateway_installation_id"))
			}
			list[existingIndex] = updated
			answer := jsonx.Obj("id", agentID, "status", "online", "is_new", false)
			if owned {
				answer.Set("token", existing.Value("token"))
			}
			return answer, nil
		}

		if admission != nil {
			if err := m.admitRegistration(ctx, system, data, admission); err != nil {
				return nil, err
			}
			requested = data.Value("profile_id")
		}
		agentID := fmt.Sprintf("agent_%d_%s", int64(math.Floor(pyTimestamp(clock())*1000)), tokenHex(3))
		token := tokenURLSafe(24)
		profile := requested
		if !jsonx.Truthy(profile) {
			id, err := m.defaultProfileID(ctx)
			if err != nil {
				return nil, err
			}
			profile = id
		}
		agent := jsonx.Obj(
			"id", agentID,
			"name", name,
			"host", host,
			"port", port,
			"token", token,
			"service_type", serviceType,
			"profile_id", profile,
			"deployment_method", data.GetOr("deployment_method", "unknown"),
			"status", "online",
			"last_heartbeat", nowISO(),
			"version", data.GetOr("version", "1.0.0"),
			"config_version", "0",
			"enabled", true,
			"created_at", nowISO(),
		)
		if jsonx.Truthy(installationID) {
			// Trusted caller metadata, never read from registration JSON.
			agent.Set("gateway_installation_id", installationID)
		}
		*agents = append(list, agent)
		return jsonx.Obj("id", agentID, "status", "online", "is_new", true, "token", token), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*jsonx.Object), nil
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
	agents, err := m.agents(ctx, true)
	if err != nil {
		return nil, err
	}
	for _, agent := range agents {
		if agent.Value("token") == token {
			return agent, nil
		}
	}
	return nil, nil
}

var editableFields = []string{"name", "host", "port", "enabled", "service_type", "profile_id"}
var guardedFields = []string{"host", "port", "enabled", "service_type", "profile_id"}

// UpdateAgent is update_agent(agent_id, updates): the editable fields only;
// false when the Agent does not exist.
func (m *Manager) UpdateAgent(ctx context.Context, agentID string, updates *jsonx.Object) (bool, error) {
	if value, ok := updates.Get("profile_id"); ok {
		if err := validateProfileID(value); err != nil {
			return false, err
		}
	}
	changed := func(agent *jsonx.Object) bool {
		for _, key := range guardedFields {
			if value, ok := updates.Get(key); ok && !jsonx.Equal(value, agent.Value(key)) {
				return true
			}
		}
		return false
	}
	profileArg, _ := updates.Value("profile_id").(string)
	result, err := m.UpdateAgents(ctx, profileArg, func(ctx context.Context, agents *[]any) (any, error) {
		for _, item := range *agents {
			agent, _ := item.(*jsonx.Object)
			if !jsonx.Equal(agent.Value("id"), agentID) {
				continue
			}
			if upgradeBusy(agent) {
				return nil, &DeploymentConflict{Message: "Agent update is in progress; wait for confirmation before editing it"}
			}
			if dnsFollowBusy(*agents, agentID) && changed(agent) {
				return nil, &DeploymentConflict{Message: "DNS 联动发布正在执行，请等待结果后再修改 Agent"}
			}
			if (haEnabled(agent) || haBusy(agent)) && changed(agent) {
				return nil, &DeploymentConflict{Message: OxidnsHALocked}
			}
			if value, ok := updates.Get("profile_id"); ok {
				current, err := m.profileOf(ctx, agent)
				if err != nil {
					return nil, err
				}
				// Runs under the same repository transaction lock used by
				// BeginDeployment, so a concurrent preparation cannot race.
				if !jsonx.Equal(value, current) && unfinishedDeployment(agent) {
					return nil, &DeploymentConflict{Message: "Agent has an unfinished deployment; finish or recover it before changing profile"}
				}
			}
			for _, field := range editableFields {
				if value, ok := updates.Get(field); ok {
					agent.Set(field, value)
				}
			}
			agent.Set("updated_at", nowISO())
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return false, err
	}
	return result.(bool), nil
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
		return nil, repo.NewValidationError("请选择其他配置空间")
	}
	expected, ok := expectedAgentIDs.([]any)
	valid := ok && len(expected) > 0
	expectedSet := map[string]bool{}
	for _, item := range expected {
		text, isString := item.(string)
		if !isString || text == "" || expectedSet[text] {
			valid = false
			break
		}
		expectedSet[text] = true
	}
	if !valid {
		return nil, repo.NewValidationError("请提供已确认的 Agent ID 列表")
	}
	result, err := m.UpdateAgents(ctx, "", func(ctx context.Context, agents *[]any) (any, error) {
		sourceMeta, err := m.repository.ProfileMetadata(ctx, source)
		if err != nil {
			return nil, err
		}
		targetMeta, err := m.repository.ProfileMetadata(ctx, target)
		if err != nil {
			return nil, err
		}
		defaultID, err := m.defaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
		var bound []*jsonx.Object
		boundIDs := map[string]bool{}
		for _, item := range *agents {
			agent, _ := item.(*jsonx.Object)
			if jsonx.Equal(truthyOr(agent.Value("profile_id"), defaultID), source) {
				bound = append(bound, agent)
				boundIDs[jsonx.Str(agent.Value("id"))] = true
			}
		}
		same := len(boundIDs) == len(expectedSet)
		for id := range boundIDs {
			same = same && expectedSet[id]
		}
		if !same {
			return nil, repo.NewInUse("Agent 绑定已变化，请刷新列表后重新确认", nil)
		}
		var blocked []string
		for _, agent := range bound {
			if upgradeBusy(agent) || haBusy(agent) || unfinishedDeployment(agent) {
				blocked = append(blocked, jsonx.Str(truthyOr(agent.Value("name"), agent.Value("id"))))
			}
		}
		if len(blocked) > 0 {
			return nil, &DeploymentConflict{Message: "以下 Agent 正在发布、更新或等待恢复，请完成后再改绑：" + strings.Join(blocked, "、")}
		}
		summaries := make([]any, 0, len(bound))
		for _, agent := range bound {
			summaries = append(summaries, repo.AgentBindingSummary(agent))
		}
		result := jsonx.Obj("from_profile", sourceMeta, "to_profile", targetMeta, "agents", summaries)
		for _, agent := range bound {
			agent.Set("profile_id", target)
			agent.Set("updated_at", nowISO())
		}
		return result, nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*jsonx.Object), nil
}

// DeleteAgent is delete_agent(agent_id): false when unknown. The heartbeat
// state, metrics and traffic history (#21 hooks) are forgotten as well.
func (m *Manager) DeleteAgent(ctx context.Context, agentID string) (bool, error) {
	var deleted *jsonx.Object
	result, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent != nil && upgradeBusy(agent) {
			return nil, &DeploymentConflict{Message: "Agent 正在更新或等待恢复"}
		}
		if dnsFollowBusy(*agents, agentID) {
			return nil, &DeploymentConflict{Message: "DNS 联动发布正在执行，请等待结果后再删除 Agent"}
		}
		if haEnabled(agent) || haBusy(agent) {
			return nil, &DeploymentConflict{Message: OxidnsHALocked}
		}
		kept := make([]any, 0, len(*agents))
		for _, item := range *agents {
			if object, _ := item.(*jsonx.Object); !jsonx.Equal(object.Value("id"), agentID) {
				kept = append(kept, item)
			}
		}
		removed := len(kept) < len(*agents)
		*agents = kept
		if removed {
			deleted = agent
		}
		return removed, nil
	})
	if err != nil {
		return false, err
	}
	if deleted != nil {
		logger.Infof("已删除 Agent %s", agentLabel(deleted))
		m.runtime.Delete(agentID)
		h := currentHooks()
		// 删除失败时文件会在下次启动时清理
		if h.DeleteMetricsHistory != nil {
			h.DeleteMetricsHistory(m, agentID)
		}
		if h.DeleteTrafficHistory != nil {
			if err := h.DeleteTrafficHistory(m, agentID); err != nil {
				// 下次保存时按 Agent 列表清理
				logger.Warningf("删除 Agent %s 的流量记录失败：%s", agentID, err)
			}
		}
	}
	return result.(bool), nil
}

// UpdateHeartbeat is update_heartbeat(agent_id, heartbeat_data): the state is
// kept in memory only; version and config_version are written to the
// configuration only when they change. false when the Agent is unknown (or
// was deleted meanwhile).
func (m *Manager) UpdateHeartbeat(ctx context.Context, agentID string, heartbeat *jsonx.Object) (bool, error) {
	if heartbeat == nil {
		return m.ObserveHeartbeat(ctx, agentID, nil)
	}
	fields, err := agentModel(heartbeat)
	if err != nil {
		return false, err
	}
	return m.ObserveHeartbeat(ctx, agentID, fields)
}

// recordHeartbeat is the seam tests use to interleave a delete.
func (m *Manager) recordHeartbeat(agent, payload *jsonx.Object, mark int64) bool {
	if testRecordHeartbeat != nil {
		return testRecordHeartbeat(m, agent, payload, mark)
	}
	return m.runtime.RecordHeartbeat(agent, payload, mark)
}

// testFailUpdate makes UpdateAgents fail (tests; Python patched
// repository.update_system_transaction).
var testFailUpdate func() error

var testRecordHeartbeat func(m *Manager, agent, payload *jsonx.Object, mark int64) bool

// SetDomainDiscovery is set_domain_discovery(agent_id, enabled): only mihomo
// Agents can enable it. Returns the updated live record, nil when unknown.
func (m *Manager) SetDomainDiscovery(ctx context.Context, agentID string, enabled bool) (*jsonx.Object, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return nil, nil
		}
		if enabled && agent.Value("service_type") != "mihomo" {
			return nil, &ValueError{Message: msgDomainDiscoveryMihomo}
		}
		agent.Set("domain_discovery_enabled", enabled)
		agent.Set("updated_at", nowISO())
		return agent.Clone(), nil
	})
	if err != nil || result == nil {
		return nil, err
	}
	return m.runtime.Overlay([]*jsonx.Object{result.(*jsonx.Object).DeepCopy()})[0], nil
}

// SetTrafficHistory is set_traffic_history(agent_id, enabled): only mihomo
// Agents; enabling records the start time. nil when unknown.
func (m *Manager) SetTrafficHistory(ctx context.Context, agentID string, enabled bool) (*jsonx.Object, error) {
	result, err := m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		agent, _ := findAgent(*agents, agentID)
		if agent == nil {
			return nil, nil
		}
		if enabled && agent.Value("service_type") != "mihomo" {
			return nil, &ValueError{Message: "流量记录只支持 mihomo Agent"}
		}
		if enabled && !jsonx.Truthy(agent.Value("traffic_history_enabled")) {
			agent.Set("traffic_history_since", utcNowISO())
		}
		agent.Set("traffic_history_enabled", enabled)
		agent.Set("updated_at", nowISO())
		return agent.Clone(), nil
	})
	if err != nil || result == nil {
		return nil, err
	}
	return m.runtime.Overlay([]*jsonx.Object{result.(*jsonx.Object).DeepCopy()})[0], nil
}
