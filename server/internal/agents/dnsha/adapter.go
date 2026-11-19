package dnsha

// The legacy registry/repository bridge is confined to this file.
import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/model"
)

var logger = logx.Get("backend.agents.oxidns_ha_publish")

func decodeAgent(raw *agents.Record) (*agent, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return modelAgent(&a)
}

func modelAgent(a *model.Agent) (*agent, error) {
	if a == nil {
		return nil, nil
	}
	target, err := deploy.NewTypedTarget(a)
	if err != nil {
		return nil, err
	}
	var fields model.Fields
	for _, key := range []string{"host", "port", "profile_id", "service_type", "enabled"} {
		data, present, err := a.MemberJSON(key)
		if err != nil {
			return nil, err
		}
		if !present {
			data = []byte("null")
		}
		if err = fields.Set(key, data); err != nil {
			return nil, err
		}
	}
	data, err := fields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var identity model.HAIdentity
	if err = json.Unmarshal(data, &identity); err != nil {
		return nil, err
	}
	ha, _, err := a.MemberJSON("oxidns_ha")
	if err != nil {
		return nil, err
	}
	if ha == nil {
		ha = []byte("null")
	}
	_, hasState, err := a.MemberJSON("oxidns_ha_state")
	if err != nil {
		return nil, err
	}
	return &agent{Agent: a, target: target, identity: identity, haRaw: ha, enabled: agents.AgentEnabledModel(a), hasHAState: hasState}, nil
}
func (s *Service) all(ctx context.Context) ([]*agent, error) {
	records, err := s.m.ReadAgents(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*agent, 0, len(records))
	for i := range records {
		if !agents.FreshHeartbeat(records[i].LastHeartbeat, nil) {
			records[i].Status = "offline"
		}
		a, err := modelAgent(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
func (s *Service) get(ctx context.Context, id string) (*agent, error) {
	record, err := s.m.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	return modelAgent(record)
}
func (s *Service) pair(ctx context.Context, id string) ([]*agent, error) {
	records, err := s.m.ReadHAPair(ctx, id)
	if err != nil {
		if errors.Is(err, agents.ErrPairIncomplete) {
			return nil, refuse(err.Error())
		}
		return nil, err
	}
	out := make([]*agent, 0, len(records))
	for i := range records {
		a, err := modelAgent(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

var owned = []string{"oxidns_ha", "oxidns_ha_state", "oxidns_ha_publication", "rule_sync", "deployments", "latest_deployment"}

func ownedBytes(a *agent) ([][]byte, error) {
	vs := []any{a.OxiDNSHA, a.OxiDNSHAState, a.OxiDNSHAPublication, a.RuleSync, a.Deployments, a.LatestDeployment}
	out := make([][]byte, len(vs))
	for i, v := range vs {
		b, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		out[i] = b
	}
	return out, nil
}
func (s *Service) update(ctx context.Context, fn func([]*agent) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.m.EditAgents(ctx, "", func(tx context.Context, rows *[]model.Agent) error {
		list := make([]*agent, 0, len(*rows))
		for i := range *rows {
			copy := (*rows)[i]
			a, err := modelAgent(&copy)
			if err != nil {
				return err
			}
			list = append(list, a)
		}
		if err := fn(list); err != nil {
			return err
		}
		for i, a := range list {
			record := &(*rows)[i]
			record.OxiDNSHA, record.OxiDNSHAState, record.OxiDNSHAPublication = a.OxiDNSHA, a.OxiDNSHAState, a.OxiDNSHAPublication
			record.RuleSync, record.Deployments, record.LatestDeployment = a.RuleSync, a.Deployments, a.LatestDeployment
			if a.dropHA {
				for _, key := range owned[:3] {
					if err := record.Clear(key); err != nil {
						return err
					}
				}
			}
		}
		return tx.Err()
	})
}
func (s *Service) source(ctx context.Context, a *agent, snapshot bool) (*deploy.Source, *deploy.Revision, error) {
	id := a.ProfileID
	var e error
	if id == "" {
		id, e = s.m.Repository().DefaultProfileID(ctx)
		if e != nil {
			return nil, nil, e
		}
	}
	if snapshot {
		r, rev, e := s.m.Repository().GetPublishSnapshot(ctx, id, false)
		if e != nil {
			return nil, nil, e
		}
		return deploy.NewSource(r, &rev), &deploy.Revision{Value: rev}, nil
	}
	r, e := s.m.Repository().GetCompatConfig(ctx, id, nil)
	if e != nil {
		return nil, nil, e
	}
	return deploy.NewSource(r, nil), nil, nil
}
func (s *Service) safe(ctx context.Context, text string) string {
	rows, e := s.m.GetAllAgents(ctx, false)
	if e != nil {
		return msgUnexpected
	}
	tokens := make([]string, 0, len(rows))
	for _, a := range rows {
		tokens = append(tokens, a.GetString("token"))
	}
	return agents.FailureText(text, jsonx.ListOf(tokens), 0)
}

// FailureMessage sanitizes user errors against every registry credential.
func (s *Service) FailureMessage(ctx context.Context, e error) string {
	if e == nil {
		return ""
	}
	return s.safe(ctx, e.Error())
}
func rawObject(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func equal(a, b any) bool {
	va, ea := jsonx.Parse(rawObject(a))
	vb, eb := jsonx.Parse(rawObject(b))
	return ea == nil && eb == nil && jsonx.Equal(va, vb)
}
func summary(j *model.HAJournal) *model.HAPublicationSummary {
	if j == nil || j.ID == "" {
		return nil
	}
	b := journalBytes(j)
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(b, &fields)
	for k := range fields {
		switch k {
		case "id", "status", "phase", "message", "owner_agent_id", "owner_deployment_id", "active_agent_id", "members", "updated_at", "warnings", "routers":
		default:
			delete(fields, k)
		}
	}
	var out model.HAPublicationSummary
	_ = json.Unmarshal(rawObject(fields), &out)
	return &out
}
func resultModel(r *agents.Result) (model.Deployment, error) {
	var d model.Deployment
	b, e := json.Marshal(r)
	if e == nil {
		e = json.Unmarshal(b, &d)
	}
	return d, e
}
func (s *Service) poll(ctx context.Context, id, did string) (model.Deployment, error) {
	r, e := s.m.GetDeploymentDirect(ctx, id, did)
	if e != nil {
		return model.Deployment{}, e
	}
	return resultModel(r)
}
func (s *Service) activateChild(ctx context.Context, id, did string) (model.Deployment, error) {
	if e := ctx.Err(); e != nil {
		return model.Deployment{}, e
	}
	r, e := s.m.ActivateDeploymentDirect(ctx, id, did)
	if e != nil {
		return model.Deployment{}, e
	}
	return resultModel(r)
}
func (s *Service) beginChildren(ctx context.Context, j *model.HAJournal) error {
	for id, did := range j.Children.All() {
		if e := ctx.Err(); e != nil {
			return e
		}
		r, e := s.m.BeginDeployment(ctx, id, did)
		if e != nil {
			return e
		}
		s.m.FinishDeploymentPreparation(id, did)
		if !r.GetBool("success") {
			m := r.GetString("message")
			if m == "" {
				m = msgReserve
			}
			return refuse(m)
		}
	}
	return nil
}
func (s *Service) retire(ctx context.Context, id, did, message string, only bool) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e := s.m.RecordDeployment(ctx, id, did, jsonx.Obj("success", false, "status", "failed", "message", message), agents.RecordOptions{OnlyUnsubmitted: only})
	return e
}
func aggregate(d *model.Deployment, p *model.HAPublicationSummary) *agents.Result {
	r, _ := jsonx.Parse(rawObject(d))
	pub, _ := jsonx.Parse(rawObject(p))
	rr, _ := r.(*jsonx.Object)
	pp, _ := pub.(*jsonx.Object)
	return agents.HAAggregate(rr, pp)
}
func conflict(a *agent) *agents.Result {
	if !active(a) {
		return nil
	}
	p, _ := jsonx.Parse(rawObject(summary(a.OxiDNSHAPublication)))
	return jsonx.Obj("success", false, "http_status", 409, "message", msgConflict, "ha_publication", p)
}
func rejection(message string) *agents.Result {
	return jsonx.Obj("success", false, "http_status", 409, "message", message)
}
func (s *Service) capabilities(ctx context.Context, a *agent) (json.RawMessage, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	timeout := agents.Seconds(5)
	r, e := s.m.DeploymentRequest(ctx, a.target.Record, "GET", "capabilities", nil, nil, &timeout)
	if e != nil {
		return nil, e
	}
	return json.Marshal(r)
}
func (s *Service) routersUsing(ctx context.Context, ids []string) ([]model.HARouter, error) {
	out := []model.HARouter{}
	ps, e := s.m.Repository().ListProfiles(ctx)
	if e != nil {
		return nil, e
	}
	for _, v := range ps {
		p, ok := v.(*jsonx.Object)
		if !ok {
			continue
		}
		profile := p.GetString("id")
		r, e := s.m.Repository().GetProfile(ctx, profile)
		if e != nil {
			return nil, e
		}
		var profileView model.Profile
		if e = json.Unmarshal(rawObject(r), &profileView); e != nil {
			return nil, e
		}
		for _, item := range profileView.Routers.Items {
			match := false
			for _, id := range ids {
				if item.DNSAgentID == id {
					match = true
				}
			}
			if match {
				name := item.Name
				if name == "" {
					name = item.ID
				}
				out = append(out, model.HARouter{ID: item.ID, Name: name, ProfileID: profile})
			}
		}
	}
	return out, nil
}

// Wire installs all cross-module hooks, with manager dispatch at the boundary.
func Wire(m *agents.Manager) { ForManager(m) }
func installHooks() {
	deploy.SetPairPublisher(func(ctx context.Context, m *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*agents.Result, bool, error) {
		return ForManager(m).Publish(ctx, id, b, o)
	})
	agents.RegisterHooks(agents.Hooks{
		HADeployment: func(ctx context.Context, m *agents.Manager, id, did string) (*agents.Result, error) {
			return ForManager(m).Deployment(ctx, id, did)
		},
		HAActivate: func(ctx context.Context, m *agents.Manager, id, did string) (*agents.Result, error) {
			return ForManager(m).Activate(ctx, id, did)
		},
		HAServiceOperation: func(ctx context.Context, m *agents.Manager, id string, op func() (*agents.Result, error), uninstall bool) (*agents.Result, error) {
			return ForManager(m).ServiceOperation(ctx, id, op, uninstall)
		},
	})
	monitor.SetHARules(func(ctx context.Context, m monitor.Manager, id, method string) (*agents.Result, int, error) {
		bridge, ok := m.(interface{ RegistryManager() *agents.Manager })
		if !ok {
			return nil, 0, errors.New("HA rules require registry manager")
		}
		real := bridge.RegistryManager()
		r, code, e := ForManager(real).Rules(ctx, id, method)
		if IsUserError(e) {
			return nil, 0, &monitor.HABusyError{Message: ForManager(real).FailureMessage(ctx, e)}
		}
		return r, code, e
	})
}

func rulesResult(id, status, phase string, j *model.HAJournal) *agents.Result {
	task := jsonx.Obj("id", id, "status", status, "phase", phase, "downloaded", 0, "download_total", 0, "reloaded", 0, "reload_total", 0)
	if status == "failed" {
		task.Set("code", "RULE_UPDATE_FAILED")
		task.Set("message", j.Message)
	}
	r := jsonx.Obj("success", true, "task", task)
	if j != nil {
		v, _ := jsonx.Parse(rawObject(summary(j)))
		r.Set("ha_publication", v)
	}
	return r
}

func truthyJSON(raw json.RawMessage) bool {
	v, e := jsonx.Parse(raw)
	return e == nil && jsonx.Truthy(v)
}
