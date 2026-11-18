package dnsfollow

// This file alone adapts the legacy repository/registry/event/task APIs.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

var logger = logx.Get("backend.agents.dns_follow")
var errUnchanged = errors.New("follow state unchanged")

func (s *Service) decodeAgent(ctx context.Context, raw *agents.Record) (*agent, error) {
	if raw == nil {
		return nil, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err = json.Unmarshal(encoded, &a); err != nil {
		return nil, err
	}
	if a.ProfileID == "" {
		a.ProfileID, err = s.m.Repository().DefaultProfileID(ctx)
		if err != nil {
			return nil, err
		}
	}
	binding, _ := json.Marshal(raw.Value("dns_follow"))
	state, _ := json.Marshal(raw.Value("dns_follow_state"))
	target := deploy.NewTarget(raw)
	target.ProfileID = a.ProfileID
	return &agent{Agent: &a, target: target, bindingRaw: binding, stateRaw: state, isEnabled: jsonx.Truthy(raw.GetOr("enabled", true))}, nil
}
func (s *Service) all(ctx context.Context) ([]*agent, error) {
	rows, err := s.m.GetAllAgents(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]*agent, 0, len(rows))
	for _, r := range rows {
		a, e := s.decodeAgent(ctx, r)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func (s *Service) get(ctx context.Context, id string) (*agent, error) {
	a, e := s.m.GetAgentByID(ctx, id, true)
	if e != nil {
		return nil, e
	}
	return s.decodeAgent(ctx, a)
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
	if e := ctx.Err(); e != nil {
		return e
	}
	_, err := s.m.UpdateAgents(ctx, "", func(tx context.Context, rows *[]any) (any, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		list := make([]*agent, 0, len(*rows))
		raws := make([]*agents.Record, 0, len(*rows))
		before := make([][3][]byte, 0, len(*rows))
		for _, v := range *rows {
			r, ok := v.(*jsonx.Object)
			if !ok {
				continue
			}
			a, e := s.decodeAgent(tx, r)
			if e != nil {
				return nil, e
			}
			list = append(list, a)
			raws = append(raws, r)
			b, _ := json.Marshal(a.DNSFollow)
			st, _ := json.Marshal(a.DNSFollowState)
			j, _ := json.Marshal(a.DNSFollowJob)
			before = append(before, [3][]byte{b, st, j})
		}
		if e := fn(tx, list); e != nil {
			return nil, e
		}
		for i, a := range list {
			members := []struct {
				key   string
				value any
				drop  bool
			}{{"dns_follow", a.DNSFollow, a.dropBinding}, {"dns_follow_state", a.DNSFollowState, a.dropState}, {"dns_follow_job", a.DNSFollowJob, a.dropJob}}
			for j, m := range members {
				if m.drop {
					raws[i].Delete(m.key)
					continue
				}
				b, e := json.Marshal(m.value)
				if e != nil {
					return nil, e
				}
				if bytes.Equal(b, before[i][j]) {
					continue
				}
				v, e := jsonx.Parse(b)
				if e != nil {
					return nil, e
				}
				raws[i].Set(m.key, v)
			}
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		return nil, nil
	})
	return err
}

func (s *Service) config(ctx context.Context, a *agent) (*deploy.Source, error) {
	raw, err := repo.GetConfig(ctx, a.ProfileID)
	if err != nil {
		return nil, err
	}
	return deploy.NewSource(raw, nil), nil
}
func (s *Service) snapshot(ctx context.Context, profile string) (*deploy.Source, *deploy.Revision, error) {
	raw, revision, e := s.m.Repository().GetPublishSnapshot(ctx, profile, false)
	if e != nil {
		return nil, nil, e
	}
	return deploy.NewSource(raw, &revision), &deploy.Revision{Value: revision}, nil
}
func activeHA(a *agent) bool { return a != nil && agents.HAActive(a.target.Record) }
func haEnabled(a *agent) bool {
	return a != nil && a.target.Record.GetObject("oxidns_ha").Value("enabled") == true
}
func (s *Service) poll(ctx context.Context, a *agent, id string) (model.Deployment, error) {
	raw, e := s.m.GetDeployment(ctx, a.ID, id)
	var d model.Deployment
	if e == nil {
		b, err := json.Marshal(raw)
		if err != nil {
			return d, err
		}
		e = json.Unmarshal(b, &d)
	}
	return d, e
}
func (s *Service) reserve(ctx context.Context, a *agent, id string) error {
	raw, e := s.m.BeginDeployment(ctx, a.ID, id)
	if e != nil {
		return e
	}
	if raw == nil || !raw.GetBool("success") || raw.GetBool("existing") {
		msg := msgReserved
		if raw != nil && raw.GetString("message") != "" {
			msg = raw.GetString("message")
		}
		return refusal(msg)
	}
	return nil
}
func (s *Service) failPreparation(ctx context.Context, a *agent, id, message string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e := s.m.RecordDeployment(ctx, a.ID, id, jsonx.Obj("status", "failed", "success", false, "message", msgPrepareFailed, "error_details", message), agents.RecordOptions{OnlyUnsubmitted: true})
	return e
}

type deviceReply struct {
	Success      bool            `json:"success"`
	HTTPStatus   int             `json:"http_status"`
	Message      string          `json:"message"`
	AgentID      string          `json:"agent_id"`
	DeploymentID string          `json:"deployment_id"`
	Enabled      bool            `json:"enabled"`
	Host         string          `json:"host"`
	Port         json.RawMessage `json:"port"`
	PeerAgentID  string          `json:"peer_agent_id"`
	ServiceType  string          `json:"service_type"`
}

func (s *Service) request(ctx context.Context, a *agent, path, deploymentID string) (deviceReply, error) {
	var out deviceReply
	if e := ctx.Err(); e != nil {
		return out, e
	}
	if deploymentID != "" {
		path += "?" + url.Values{"deployment_id": {deploymentID}}.Encode()
	}
	raw, err := s.m.DeploymentRequest(ctx, a.target.Record, http.MethodGet, path, nil, nil, nil)
	if err != nil {
		var transport *agents.TransportError
		if errors.As(err, &transport) {
			return out, refusal(fmt.Sprintf(msgUnreachable, name(a)))
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
	if !out.Success || out.HTTPStatus != 0 {
		message := out.Message
		if message == "" || out.HTTPStatus == 404 {
			message = msgUpgrade
		}
		return out, refusal(fmt.Sprintf(msgAgentFailure, name(a), message))
	}
	return out, nil
}
func (s *Service) tokens(list []*agent) []any {
	secrets := redact.Secrets{}
	for _, a := range list {
		secrets = redact.CollectCredentials(a.target.Record, secrets)
		secrets = append(secrets, a.Token)
	}
	out := make([]any, len(secrets))
	for i, v := range secrets {
		out[i] = v
	}
	return out
}
func (s *Service) safe(message string, list []*agent) string {
	return agents.FailureText(message, s.tokens(list), 0)
}

// FailureMessage uses all current Agent credentials, including private proof
// values, before exposing a refusal through an HTTP or task response.
func (s *Service) FailureMessage(ctx context.Context, err error) string {
	rows, e := s.m.GetAllAgents(ctx, true)
	if e != nil {
		return agents.FailureText(err.Error(), nil, 0)
	}
	secrets := redact.Secrets{}
	for _, r := range rows {
		secrets = redact.CollectCredentials(r, secrets)
		secrets = append(secrets, r.GetString("token"))
	}
	values := make([]any, len(secrets))
	for i, v := range secrets {
		values[i] = v
	}
	return agents.FailureText(err.Error(), values, 0)
}
func (s *Service) publicOptions(v OptionsView, list []*agent) (OptionsView, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return v, e
	}
	raw, e := jsonx.Parse(b)
	if e != nil {
		return v, e
	}
	secrets := redact.Secrets{}
	for _, a := range list {
		secrets = redact.CollectCredentials(a.target.Record, secrets)
		secrets = append(secrets, a.Token)
	}
	b, e = json.Marshal(redact.SanitizeCredentials(raw.(*jsonx.Object), secrets))
	if e != nil {
		return v, e
	}
	e = json.Unmarshal(b, &v)
	return v, e
}
func (s *Service) appendEvents(events []model.OperationEvent, status, message, at string, list []*agent) ([]model.OperationEvent, error) {
	b, e := json.Marshal(events)
	if e != nil {
		return nil, e
	}
	raw, e := jsonx.Parse(b)
	if e != nil {
		return nil, e
	}
	next := agents.AppendEvent(raw, status, message, at, s.tokens(list))
	b, e = json.Marshal(next)
	if e != nil {
		return nil, e
	}
	var out []model.OperationEvent
	e = json.Unmarshal(b, &out)
	return out, e
}
func (s *Service) recordOutcome(ctx context.Context, id, status, message string) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	store := notifications.TaskStatus(ctx, s.m.Repository().DataDir())
	store.Now = s.now
	_, err := store.Record("dns_follow:"+id, taskstatus.Result{OK: status == "succeeded", Trigger: "event", Error: message})
	return err
}
func (s *Service) logState(a *agent, previous model.DNSFollowState, current model.DNSFollowState) {
	if previous.Status == current.Status && previous.Message == current.Message {
		return
	}
	label := logx.LogText(name(a), 0)
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
	_, _, e := s.m.Repository().UpdateProfileTransaction(ctx, profile, func(tx context.Context, raw *jsonx.Object) (*jsonx.Object, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		next, e := fn(tx, deploy.NewSource(raw, nil))
		if e != nil {
			return nil, e
		}
		if next == nil {
			return nil, errUnchanged
		}
		b, e := next.Raw()
		if e != nil {
			return nil, e
		}
		v, e := jsonx.Parse(b)
		if e != nil {
			return nil, e
		}
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		return v.(*jsonx.Object), nil
	})
	if errors.Is(e, errUnchanged) {
		return nil
	}
	return e
}

func taskRow(ctx context.Context, raw *agents.Record, statuses *jsonx.Object) (*jsonx.Object, error) {
	if raw.GetString("service_type") != "oxidns" {
		return nil, nil
	}
	id := raw.GetString("id")
	key := "dns_follow:" + id
	isEnabled := jsonx.Truthy(raw.Value("dns_follow"))
	if !isEnabled && !statuses.Has(key) {
		return nil, nil
	}
	state := raw.GetObject("dns_follow_state")
	status := state.GetString("status")
	history := tasks.History(statuses.GetObject(key))
	if status == "succeeded" || status == "failed" {
		last := "success"
		detail := ""
		if status == "failed" {
			last = "failed"
			detail = state.GetString("message")
		}
		history.Set("last_status", last)
		history.Set("last_error", detail)
	}
	label := raw.GetString("name")
	if label == "" {
		label = id
	}
	hint := msgRunHint
	if status == "failed" {
		hint = ""
	}
	message := ""
	if status == "waiting" || running(status) {
		message = state.GetString("message")
	}
	row := tasks.Row(key, "dns_follow", label, "resource_id", id, "enabled", isEnabled, "schedule_text", msgSchedule, "can_run", isEnabled && status == "failed", "run_hint", hint, "running", running(status), "link", "/agents", "state_message", message)
	row.Update(history)
	return row, nil
}

func timestamp(t time.Time) string {
	t = t.UTC().Truncate(time.Microsecond)
	if t.Nanosecond() == 0 {
		return t.Format("2006-01-02T15:04:05Z")
	}
	return t.Format("2006-01-02T15:04:05.000000Z")
}
