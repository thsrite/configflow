package dnsha

// Native registry/repository collaborators and typed HA result envelopes.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/deploy"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

var logger = logx.Get("backend.agents.oxidns_ha_publish")

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
		if !agents.AgentMatchesID(&records[i], records[i].ID) {
			continue
		}
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
	id, err := s.m.ResolveAgentProfile(ctx, a.Agent)
	if err != nil {
		return nil, nil, err
	}
	var config *model.Config
	var revision int64
	if snapshot {
		config, revision, err = s.m.Repository().PublishConfig(ctx, id, false)
	} else {
		config, err = s.m.Repository().Config(ctx, id, nil)
	}
	if err != nil {
		return nil, nil, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, nil, err
	}
	if snapshot {
		source, err := deploy.SourceFromJSON(raw, &revision)
		return source, &deploy.Revision{Value: revision}, err
	}
	source, err := deploy.SourceFromJSON(raw, nil)
	return source, nil, err
}
func (s *Service) safe(ctx context.Context, text string) string {
	tokens, err := s.m.Repository().AgentTokensForSanitization(ctx)
	if err != nil {
		return msgUnexpected
	}
	values := make([]string, 0, len(tokens))
	for token := range tokens {
		values = append(values, token)
	}
	return redact.DiagnosticText(text, redact.NewSecrets(values), 0)
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
	left, err := yamlx.FromJSON(rawObject(a))
	if err != nil {
		return false
	}
	right, err := yamlx.FromJSON(rawObject(b))
	return err == nil && yamlx.Equal(left, right)
}
func summary(j *model.HAJournal) *model.HAPublicationSummary {
	if j == nil || j.ID == "" {
		return nil
	}
	result, err := agents.HASummaryModel(j)
	if err != nil {
		return nil
	}
	return result
}
func (s *Service) poll(ctx context.Context, id, did string) (model.Deployment, error) {
	result, err := s.m.ReadDeploymentModel(ctx, id, did)
	if err != nil {
		return model.Deployment{}, err
	}
	return *result, nil
}
func (s *Service) activateChild(ctx context.Context, id, did string) (model.Deployment, error) {
	result, err := s.m.ActivateCandidateModel(ctx, id, did)
	if err != nil {
		return model.Deployment{}, err
	}
	return *result, nil
}
func (s *Service) beginChildren(ctx context.Context, j *model.HAJournal) error {
	for _, id := range j.Children.Keys() {
		raw, present, err := j.Children.MemberJSON(id)
		var child *string
		if err != nil || !present || json.Unmarshal(raw, &child) != nil || child == nil || *child == "" {
			return refuse(msgJournalChanged)
		}
		did := *child
		if e := ctx.Err(); e != nil {
			return e
		}
		r, e := s.m.ReserveDeployment(ctx, id, did)
		if e != nil {
			return e
		}
		s.m.FinishDeploymentPreparation(id, did)
		if !r.Success {
			m := ""
			if r.Message != nil {
				m = *r.Message
			}
			if m == "" {
				m = msgReserve
			}
			return refuse(m)
		}
	}
	return nil
}
func (s *Service) retire(ctx context.Context, id, did, message string, only bool) error {
	raw, err := json.Marshal(struct {
		Success bool   `json:"success"`
		Status  string `json:"status"`
		Message string `json:"message"`
	}{false, "failed", message})
	if err != nil {
		return err
	}
	patch, err := agents.DecodeDeploymentUpdate(raw)
	if err != nil {
		return err
	}
	_, err = s.m.RecordDeploymentModel(ctx, id, did, patch, agents.RecordOptions{OnlyUnsubmitted: only})
	return err
}
func aggregate(d *model.Deployment, p *model.HAPublicationSummary) *model.Deployment {
	result, err := agents.HAAggregateModel(d, p)
	if err != nil {
		return rejection(msgUnexpected)
	}
	return result
}
func conflict(a *agent) *model.Deployment {
	if !active(a) {
		return nil
	}
	result := rejection(msgConflict)
	result.HAPublication = summary(a.OxiDNSHAPublication)
	return result
}
func rejection(message string) *model.Deployment {
	result := &model.Deployment{HTTPStatus: ptr(model.Int(409)), Message: &message}
	// Presence of false is part of the response contract; the literal is valid JSON.
	_ = result.SetJSON("success", []byte(`false`))
	return result
}
func (s *Service) capabilities(ctx context.Context, a *agent) (json.RawMessage, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	timeout := agents.Seconds(5)
	r, e := s.m.DeploymentRequestModel(ctx, a.Agent, "GET", "capabilities", nil, nil, &timeout)
	if e != nil {
		return nil, e
	}
	return json.Marshal(r)
}
func (s *Service) routersUsing(ctx context.Context, ids []string) ([]model.HARouter, error) {
	result := []model.HARouter{}
	err := s.m.Repository().View(ctx, func(document *model.Document) error {
		for i := range document.Profiles {
			profile := document.Profiles[i]
			for _, router := range profile.Routers.Items {
				raw, present, err := router.MemberJSON("dns_agent_id")
				if err != nil {
					return err
				}
				var id *string
				if !present || json.Unmarshal(raw, &id) != nil || id == nil {
					continue
				}
				match := false
				for _, target := range ids {
					if *id == target {
						match = true
						break
					}
				}
				if !match {
					continue
				}
				name := router.Name
				if name == "" {
					name = router.ID
				}
				result = append(result, model.HARouter{ID: router.ID, Name: name, ProfileID: profile.Key()})
			}
		}
		return nil
	})
	return result, err
}

// Wire installs all cross-module hooks, with manager dispatch at the boundary.
func Wire(m *agents.Manager) { ForManager(m) }
func installHooks() {
	deploy.SetPairPublication(func(ctx context.Context, m *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
		return ForManager(m).PublishPair(ctx, id, b, o)
	})
	agents.RegisterHooks(agents.Hooks{
		HADeployment: func(ctx context.Context, m *agents.Manager, id, did string) (*model.Deployment, error) {
			return ForManager(m).PairDeployment(ctx, id, did)
		},
		HAActivate: func(ctx context.Context, m *agents.Manager, id, did string) (*model.Deployment, error) {
			return ForManager(m).ActivatePair(ctx, id, did)
		},
		HAServiceOperationTyped: func(ctx context.Context, m *agents.Manager, id string, op func() (json.RawMessage, error), uninstall bool) (json.RawMessage, error) {
			return ForManager(m).ServiceOperationJSON(ctx, id, op, uninstall)
		},
	})
	monitor.SetHARulesJSON(func(ctx context.Context, m monitor.Registry, id, method string) (json.RawMessage, int, error) {
		bridge, ok := m.(interface{ RegistryManager() *agents.Manager })
		if !ok {
			return nil, 0, errors.New("HA rules require registry manager")
		}
		real := bridge.RegistryManager()
		r, code, e := ForManager(real).PairRules(ctx, id, method)
		if IsUserError(e) {
			return nil, 0, &monitor.HABusyError{Message: ForManager(real).FailureMessage(ctx, e)}
		}
		if e != nil {
			return nil, code, e
		}
		raw, e := json.Marshal(r)
		return raw, code, e
	})
}

// RuleResult is the Agent rule-update envelope for an HA publication.
type RuleResult struct {
	Success       bool                        `json:"success"`
	Task          monitor.RuleTask            `json:"task"`
	HAPublication *model.HAPublicationSummary `json:"ha_publication,omitempty"`
}

func rulesResult(id, status, phase string, j *model.HAJournal) *RuleResult {
	result := &RuleResult{Success: true, Task: monitor.RuleTask{ID: id, Status: status, Phase: phase}}
	if status == "failed" {
		result.Task.Code, result.Task.Message = "RULE_UPDATE_FAILED", j.Message
	}
	if j != nil {
		result.HAPublication = summary(j)
	}
	return result
}

func truthyJSON(raw json.RawMessage) bool {
	present, err := model.JSONHasContent(raw)
	return err == nil && present
}

// journalChildID requires the original token rather than Map's R5 text projection.
func journalChildID(j *model.HAJournal, id string) (string, bool) {
	if j == nil {
		return "", false
	}
	raw, present, err := j.Children.MemberJSON(id)
	var text *string
	if err != nil || !present || json.Unmarshal(raw, &text) != nil || text == nil || *text == "" {
		return "", false
	}
	return *text, true
}
func journalID(j *model.HAJournal) string {
	if j == nil {
		return ""
	}
	raw, present, err := j.MemberJSON("id")
	var id *string
	if err != nil || !present || json.Unmarshal(raw, &id) != nil || id == nil {
		return ""
	}
	return *id
}
func journalText(j *model.HAJournal, name string) (string, bool) {
	if j == nil {
		return "", false
	}
	raw, present, err := j.MemberJSON(name)
	var text *string
	if err != nil || !present || json.Unmarshal(raw, &text) != nil || text == nil || *text == "" {
		return "", false
	}
	return *text, true
}
func validJournalChildren(j *model.HAJournal) bool {
	if j == nil || journalID(j) == "" || j.Children.Len() != 2 {
		return false
	}
	for id := range j.Children.All() {
		if _, valid := journalChildID(j, id); !valid {
			return false
		}
	}
	owner, ownerOK := journalText(j, "owner_agent_id")
	deployment, deploymentOK := journalText(j, "owner_deployment_id")
	child, childOK := journalChildID(j, owner)
	if !ownerOK || !deploymentOK || !childOK || deployment != child {
		return false
	}
	if j.Enable != nil {
		raw, present, err := j.Enable.MemberJSON("peer_id")
		var peer *string
		if err != nil || !present || json.Unmarshal(raw, &peer) != nil || peer == nil || *peer == owner {
			return false
		}
		if _, valid := journalChildID(j, *peer); !valid {
			return false
		}
	}
	return true
}
func validHAIdentity(identity *model.HAIdentity) bool {
	if identity == nil {
		return false
	}
	for _, name := range []string{"host", "service_type", "profile_id"} {
		raw, present, err := identity.MemberJSON(name)
		if err != nil {
			return false
		}
		var text *string
		if present && json.Unmarshal(raw, &text) != nil {
			return false
		}
		if name != "profile_id" && (text == nil || *text == "") {
			return false
		}
	}
	return true
}

// sameHAIdentity keeps original token types except equivalent integral numeric
// port notation (8082 / 8082.0). Quoted numbers and booleans never gain that proof.
func sameHAIdentity(left, right *model.HAIdentity) bool {
	if !validHAIdentity(left) || !validHAIdentity(right) {
		return false
	}
	lraw, lp, le := left.MemberJSON("port")
	rraw, rp, re := right.MemberJSON("port")
	numeric := func(raw json.RawMessage, present bool, err error) (model.Int, bool) {
		raw = bytes.TrimSpace(raw)
		if err != nil || !present || len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
			return 0, false
		}
		var number model.Int
		err = json.Unmarshal(raw, &number)
		return number, err == nil
	}
	l, lOK := numeric(lraw, lp, le)
	r, rOK := numeric(rraw, rp, re)
	if !lOK || !rOK || l != r {
		return equal(left, right)
	}
	a, b := *left, *right
	if a.Clear("port") != nil || b.Clear("port") != nil {
		return false
	}
	a.Port, b.Port = l, r
	return equal(&a, &b)
}

func resultModel(r *agents.Result) (model.Deployment, error) {
	var d model.Deployment
	raw, err := json.Marshal(r)
	if err == nil {
		err = json.Unmarshal(raw, &d)
	}
	return d, err
}
