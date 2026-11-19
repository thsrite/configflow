package dnsfollow

// Typed repository, registry, event and task collaborators.
import (
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.agents.dns_follow")
var errUnchanged = errors.New("follow state unchanged")

func (s *Service) modelAgent(ctx context.Context, a *model.Agent) (*agent, error) {
	if a == nil {
		return nil, nil
	}
	profile, err := s.m.ResolveAgentProfile(ctx, a)
	if err != nil {
		return nil, err
	}
	return modelAgentWithProfile(a, profile)
}
func modelAgentWithProfile(a *model.Agent, profile string) (*agent, error) {
	target, err := deploy.NewTypedTarget(a)
	if err != nil {
		return nil, err
	}
	binding, _, err := a.MemberJSON("dns_follow")
	if err != nil {
		return nil, err
	}
	state, _, err := a.MemberJSON("dns_follow_state")
	if err != nil {
		return nil, err
	}
	if binding == nil {
		binding = json.RawMessage("null")
	}
	if state == nil {
		state = json.RawMessage("null")
	}
	a.ProfileID = profile
	target.ProfileID = profile
	return &agent{Agent: a, target: target, bindingRaw: binding, stateRaw: state, isEnabled: agents.AgentEnabledModel(a)}, nil
}
func (s *Service) all(ctx context.Context) ([]*agent, error) {
	rows, err := s.m.ReadAgents(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	profiles, err := s.m.ResolveAgentProfiles(ctx, rows)
	if err != nil {
		return nil, err
	}
	out := make([]*agent, 0, len(rows))
	for i := range rows {
		if !agents.AgentMatchesID(&rows[i], rows[i].ID) {
			continue
		}
		// Listing historically derives offline; individual reads keep stored status.
		if !agents.FreshHeartbeat(rows[i].LastHeartbeat, nil) {
			rows[i].Status = "offline"
		}
		a, err := modelAgentWithProfile(&rows[i], profiles[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
func (s *Service) get(ctx context.Context, id string) (*agent, error) {
	a, err := s.m.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	return s.modelAgent(ctx, a)
}
func (s *Service) require(ctx context.Context, id, service string) (*agent, error) {
	a, e := s.get(ctx, id)
	if e != nil {
		return nil, e
	}
	if !enabled(a) || a.ServiceType != service {
		return nil, refusal(service + msgMissingAgent)
	}
	return a, nil
}

// update writes only A17-owned members. Other fields and future members remain
// in their original records; the pure callback can inspect the full current list.
func (s *Service) update(ctx context.Context, fn func(context.Context, []*agent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.m.EditAgents(ctx, "", func(tx context.Context, rows *[]model.Agent) error {
		profiles, err := s.m.ResolveAgentProfiles(tx, *rows)
		if err != nil {
			return err
		}
		list := make([]*agent, 0, len(*rows))
		for i := range *rows {
			copy := (*rows)[i]
			a, err := modelAgentWithProfile(&copy, profiles[i])
			if err != nil {
				return err
			}
			list = append(list, a)
		}
		if err := fn(tx, list); err != nil {
			return err
		}
		for i, a := range list {
			record := &(*rows)[i]
			record.DNSFollow, record.DNSFollowState, record.DNSFollowJob = a.DNSFollow, a.DNSFollowState, a.DNSFollowJob
			for _, member := range []struct {
				key  string
				drop bool
			}{{"dns_follow", a.dropBinding}, {"dns_follow_state", a.dropState}, {"dns_follow_job", a.dropJob}} {
				if member.drop {
					if err := record.Clear(member.key); err != nil {
						return err
					}
				}
			}
		}
		return tx.Err()
	})
}

func (s *Service) config(ctx context.Context, a *agent) (*deploy.Source, error) {
	config, err := s.m.Repository().Config(ctx, a.target.ProfileID, nil)
	if err != nil {
		return nil, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, err
	}
	return deploy.SourceFromJSON(raw, nil)
}
func (s *Service) snapshot(ctx context.Context, profile string) (*deploy.Source, *deploy.Revision, error) {
	config, revision, err := s.m.Repository().PublishConfig(ctx, profile, false)
	if err != nil {
		return nil, nil, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, nil, err
	}
	source, err := deploy.SourceFromJSON(raw, &revision)
	return source, &deploy.Revision{Value: revision}, err
}

func activeHA(a *agent) bool { return a != nil && agents.HAActiveModel(a.Agent) }
func haEnabled(a *agent) bool {
	return a != nil && agents.HAEnabledModel(a.Agent)
}
func (s *Service) poll(ctx context.Context, a *agent, id string) (model.Deployment, error) {
	record, err := s.m.GetDeploymentModel(ctx, a.ID, id)
	if err != nil {
		return model.Deployment{}, err
	}
	if record == nil {
		return model.Deployment{}, agents.ErrInvalidAgentReply
	}
	return *record, nil
}
func (s *Service) reserve(ctx context.Context, a *agent, id string) error {
	record, err := s.m.ReserveDeployment(ctx, a.ID, id)
	if err != nil {
		return err
	}
	if record == nil || !record.Success || deploymentExisting(record) {
		message := msgReserved
		if record != nil && record.Message != nil && *record.Message != "" {
			message = *record.Message
		}
		return refusal(message)
	}
	return nil
}
func deploymentExisting(record *model.Deployment) bool {
	if record == nil {
		return false
	}
	raw, present, err := record.MemberJSON("existing")
	var flag bool
	return err == nil && present && json.Unmarshal(raw, &flag) == nil && flag
}
func (s *Service) failPreparation(ctx context.Context, a *agent, id, message string) error {
	patch, err := json.Marshal(struct {
		Status  string `json:"status"`
		Success bool   `json:"success"`
		Message string `json:"message"`
		Details string `json:"error_details"`
	}{"failed", false, msgPrepareFailed, message})
	if err != nil {
		return err
	}
	update, err := agents.DecodeDeploymentUpdate(patch)
	if err != nil {
		return err
	}
	_, err = s.m.RecordDeploymentModel(ctx, a.ID, id, update, agents.RecordOptions{OnlyUnsubmitted: true})
	return err
}

type deviceReply struct {
	Success      bool
	Failed       bool // http_status present and truthy
	HTTPStatus   int
	Message      string
	AgentID      string
	DeploymentID string
	Enabled      bool
	Host         string
	HostIsText   bool // host is a JSON string, possibly empty
	Port         json.RawMessage
	PeerAgentID  string
	ServiceType  string
}

// UnmarshalJSON reads each member on its own. A member of the wrong type is
// treated as absent instead of failing the whole reply, so a malformed
// listener answer still gets its specific refusal (R04-F1).
func (r *deviceReply) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return fmt.Errorf("decode Agent DNS reply: %w", err)
	}
	members := map[string]json.RawMessage{}
	for key, raw := range fields.All() {
		members[key] = raw
	}
	str := func(key string) string { var text string; _ = json.Unmarshal(members[key], &text); return text }
	hasContent := func(key string) bool {
		present, err := model.JSONHasContent(members[key])
		return err == nil && present
	}
	var status int
	_ = json.Unmarshal(members["http_status"], &status) // 404 only when integral
	var enabled bool
	_ = json.Unmarshal(members["enabled"], &enabled)
	var host string
	hostIsText := json.Unmarshal(members["host"], &host) == nil && string(members["host"]) != "null"
	*r = deviceReply{Success: hasContent("success"), Failed: hasContent("http_status"), HTTPStatus: status, Message: str("message"),
		AgentID: str("agent_id"), DeploymentID: str("deployment_id"), Enabled: enabled, Host: host, HostIsText: hostIsText, Port: members["port"],
		PeerAgentID: str("peer_agent_id"), ServiceType: str("service_type")}
	return nil
}

func (s *Service) request(ctx context.Context, a *agent, path, deploymentID string) (deviceReply, error) {
	var out deviceReply
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if deploymentID != "" {
		path += "?" + url.Values{"deployment_id": {deploymentID}}.Encode()
	}
	raw, err := s.m.DeploymentRequestModel(ctx, a.Agent, http.MethodGet, path, nil, nil, nil)
	if err != nil {
		var transport *agents.TransportError
		if errors.As(err, &transport) {
			return out, refusal(fmt.Sprintf(msgUnreachable, deviceName(a)))
		}
		return out, err
	}
	b, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	if err != nil {
		return out, err
	}
	if !out.Success || out.Failed {
		message := out.Message
		if message == "" || out.HTTPStatus == 404 {
			message = msgUpgrade
		}
		return out, refusal(fmt.Sprintf(msgAgentFailure, deviceName(a), message))
	}
	return out, nil
}

// deviceName labels Agent call failures: the stored name, or "Agent" when the
// record has none (Python agent.get("name", "Agent")).
func deviceName(a *agent) string {
	if text, ok := agents.AgentName(a.Agent); ok {
		return text
	}
	return msgDefaultAgentName
}
func sourceName(a *agent) string {
	if text, ok := agents.AgentName(a.Agent); ok {
		return text
	}
	return a.ID
}

func (s *Service) safe(message string, list []*agent) string {
	return redact.DiagnosticText(message, s.secrets(list), 0)
}

// FailureMessage uses all current Agent credentials, including private proof
// values, before exposing a refusal through an HTTP or task response.
func (s *Service) FailureMessage(ctx context.Context, err error) string {
	secrets := redact.Secrets{}
	lookupErr := s.m.Repository().ViewAgents(ctx, func(rows []model.Agent) error {
		for i := range rows {
			raw, err := model.Marshal(rows[i])
			if err != nil {
				return err
			}
			secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
		}
		return nil
	})
	if lookupErr != nil {
		return msgUnexpected
	}
	return redact.DiagnosticText(errText(err), secrets, 0)
}
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func (s *Service) secrets(list []*agent) redact.Secrets {
	secrets := redact.Secrets{}
	for _, a := range list {
		raw, err := json.Marshal(a.Agent)
		if err == nil {
			secrets = redact.CollectCredentials(json.RawMessage(raw), secrets)
		}
		secrets = redact.NewSecrets(secrets, []string{a.Token})
	}
	return secrets
}
func (s *Service) publicOptions(v OptionsView, list []*agent) (OptionsView, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	err = jsondecode.Unmarshal(redact.SanitizeCredentials(json.RawMessage(raw), s.secrets(list)), &v)
	return v, err
}
func (s *Service) appendEvents(events []model.OperationEvent, status, message, at string, list []*agent) ([]model.OperationEvent, error) {
	return agents.AppendOperationEvent(events, status, message, at, s.secrets(list)), nil
}

func (s *Service) recordOutcome(ctx context.Context, id, status, message string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	store := notifications.TaskStatus(ctx, s.m.Repository().DataDir())
	_, err := store.SaveResult(ctx, "dns_follow:"+id, taskstatus.Result{OK: status == "succeeded", Trigger: "event", Error: message})
	return err
}
func (s *Service) logState(a *agent, previous model.DNSFollowState, current model.DNSFollowState, list []*agent) {
	if previous.Status == current.Status && previous.Message == current.Message {
		return
	}
	label := logx.LogText(s.safe(name(a), list), 0)
	message := logx.LogText(current.Message, 300)
	switch current.Status {
	case "failed":
		logger.Warningf(msgLog, label, message)
	case "preparing", "succeeded":
		logger.Infof(msgLog, label, message)
	default:
		logger.Debugf(msgLog, label, message)
	}
}

func (s *Service) saveDraft(ctx context.Context, profile string, fn func(context.Context, *deploy.Source) (*deploy.Source, error)) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e := s.m.Repository().UpdateProfile(ctx, profile, func(tx context.Context, stored *model.Profile) error {
		if e := tx.Err(); e != nil {
			return e
		}
		data, e := model.Marshal(stored)
		if e != nil {
			return e
		}
		source, e := deploy.SourceFromJSON(data, nil)
		if e != nil {
			return e
		}
		next, e := fn(tx, source)
		if e != nil {
			return e
		}
		if next == nil {
			return errUnchanged
		}
		data, e = next.Raw()
		if e != nil {
			return e
		}
		var replacement model.Profile
		if e = json.Unmarshal(data, &replacement); e != nil {
			return e
		}
		if e = tx.Err(); e != nil {
			return e
		}
		*stored = replacement
		return nil
	})
	if errors.Is(e, errUnchanged) {
		return nil
	}
	return e
}

func agentTaskRow(ctx context.Context, a *model.Agent, statuses taskstatus.Snapshot) (*tasks.Task, error) {
	if a.ServiceType != "oxidns" {
		return nil, nil
	}
	id := a.ID
	key := "dns_follow:" + id
	raw, _, err := a.MemberJSON("dns_follow")
	if err != nil {
		return nil, err
	}
	enabled, err := model.JSONHasContent(raw)
	if err != nil {
		return nil, err
	}
	_, recorded := statuses[key]
	if !enabled && !recorded {
		return nil, nil
	}
	history, err := tasks.ProjectHistory(statuses.Entry(key))
	if err != nil {
		return nil, err
	}
	status, message := "", ""
	if state := a.DNSFollowState; state != nil {
		status, message = state.Status, state.Message
	}
	if status == "succeeded" || status == "failed" {
		last, detail := "success", ""
		if status == "failed" {
			last, detail = "failed", message
		}
		history.LastStatus, history.LastError = &last, &detail
	}
	label, _ := agents.AgentName(a)
	if label == "" {
		label = id
	}
	hint := msgRunHint
	if status == "failed" {
		hint = ""
	}
	stateMessage := ""
	if status == "waiting" || running(status) {
		stateMessage = message
	}
	link := "/agents"
	row := &tasks.Task{Key: key, Category: "dns_follow", Name: label, ResourceID: id, Enabled: enabled, ScheduleText: msgSchedule, CanRun: enabled && status == "failed", RunHint: hint, Running: running(status), Link: &link, StateMessage: stateMessage, HistoryData: history}
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := m.ReadAgents(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	secrets := redact.Secrets{}
	for i := range rows {
		data, err := model.Marshal(rows[i])
		if err != nil {
			return nil, err
		}
		secrets = redact.CollectCredentials(json.RawMessage(data), secrets)
	}
	data, err := json.Marshal(row)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(redact.SanitizeCredentials(json.RawMessage(data), secrets), row); err != nil {
		return nil, err
	}
	return row, nil
}

func timestamp(t time.Time) string {
	t = t.UTC().Truncate(time.Microsecond)
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05.000000Z")
}
