package ipv6

// Legacy repo, converter and registry types stop at this adapter. The IPv6
// owners only operate on native snapshots and independently owned Agent records.
import (
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/agents/monitor"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	healthcheck "configflow/internal/health/check"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
)

func defaultMihomo() (*yamlx.Node, error) { return convert.MihomoTemplate(), nil }

// agentRecord reads the native record used by registry and deployment owners.
func agentRecord(d Document) (*model.Agent, error) {
	var a model.Agent
	if err := json.Unmarshal(jsonresp.Value(d), &a); err != nil {
		return nil, err
	}
	return &a, nil
}
func (s *Service) snapshot(ctx context.Context, profile string) (Document, int64, error) {
	config, revision, err := s.Manager.Repository().PublishConfig(ctx, profile, true)
	if err != nil {
		return nil, 0, err
	}
	data, err := model.Marshal(config)
	return object(data), revision, err
}
func agentDocument(a *model.Agent) (Document, error) {
	if a == nil {
		return nil, nil
	}
	data, err := model.Marshal(a)
	if err != nil {
		return nil, err
	}
	return object(data), nil
}
func (s *Service) agent(ctx context.Context, id, profile string) (Document, error) {
	a, err := s.Manager.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	d, err := agentDocument(a)
	if err != nil {
		return nil, err
	}
	if d == nil {
		return nil, failure("AGENT_NOT_FOUND", 404, msgMissing)
	}
	if text(d["profile_id"]) != profile {
		return nil, failure("PROFILE_CHANGED", 409, msgProfile)
	}
	if text(d["service_type"]) != "mihomo" {
		return nil, failure("WRONG_SERVICE", 400, msgService)
	}
	if string(d["enabled"]) == "false" {
		return nil, failure("AGENT_DISABLED", 409, msgDisabled)
	}
	return d, nil
}
func (s *Service) lookup(ctx context.Context, id string) Document {
	if id == "" {
		return nil
	}
	a, err := s.Manager.ReadAgent(ctx, id, true)
	if err != nil {
		return nil
	}
	d, _ := agentDocument(a)
	return d
}
func online(a Document) bool {
	return agents.FreshHeartbeat(text(a["last_heartbeat"]), nil) && text(a["status"]) != "offline"
}
func origin(a Document) (string, error) {
	o, e := agentRecord(a)
	if e != nil {
		return "", e
	}
	return monitor.Origin(o)
}

func (s *Service) publication(ctx context.Context, a, config Document, revision int64, wait bool) string {
	applied := object(a["applied_content"])
	if text(applied["deployment_id"]) == "" || len(a["config_revision"]) == 0 || string(a["config_revision"]) == "null" {
		return "unknown"
	}
	var r int64
	if json.Unmarshal(a["config_revision"], &r) == nil && r == revision {
		return "synced"
	}
	if s.Checker == nil {
		return "unknown"
	}
	checker := s.Checker(s.Manager)
	if checker == nil {
		return "unknown"
	}
	o, e := agentRecord(a)
	if e != nil {
		return "unknown"
	}
	src, e := deploy.SourceFromJSON(jsonresp.Value(config), &revision)
	if e != nil {
		return "unknown"
	}
	target, e := deploy.NewTypedTarget(o)
	if e != nil {
		return "unknown"
	}
	var status string
	if wait {
		status = checker.CompareNow(ctx, target, src, text(applied["base_url"])).Status
	} else {
		status = checker.Status(ctx, target, src, text(applied["base_url"])).Status
	}
	if status == "synced" || status == "changed" || status == "checking" {
		return status
	}
	return "unknown"
}
func (s *Service) request(ctx context.Context, a Document, operation string, payload Document) (Document, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	address, e := origin(a)
	if e != nil {
		return nil, failure("AGENT_UNREACHABLE", 502, msgUnreachable)
	}
	method, path := http.MethodGet, "/api/ipv6-readiness"
	var body []byte
	if operation != "" {
		method = http.MethodPost
		path += "/" + operation
		body = jsonresp.Value(payload)
	}
	header := http.Header{"Authorization": {"Bearer " + text(a["token"])}}
	if operation != "" {
		header.Set("Content-Type", "application/json")
	}
	response, e := s.Manager.Client().Do(ctx, agents.Request{Method: method, URL: address + path, Header: header, Body: body, Timeout: agents.Pair(3, 45), NoProxy: true, NoRedirects: true, MaxBody: 512 * 1024})
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil || response == nil || response.BodyError != nil {
		message := msgUnreachable
		if operation == "prepare" {
			message = msgPrepareUnknown
		}
		return nil, failure("AGENT_UNREACHABLE", 502, message)
	}
	if len(response.Body) > 512*1024 {
		return nil, failure("INVALID_RESPONSE", 502, msgTooLarge)
	}
	switch response.StatusCode {
	case 404:
		return nil, failure("UPGRADE_REQUIRED", 409, msgUpgrade)
	case 401, 403:
		return nil, failure("AGENT_AUTH_FAILED", 502, msgAuth)
	}
	if !json.Valid(response.Body) {
		return nil, failure("INVALID_RESPONSE", 502, msgResponseJSON)
	}
	var fields model.Fields
	if json.Unmarshal(response.Body, &fields) != nil {
		return nil, failure("INVALID_RESPONSE", 502, msgResponseShape)
	}
	data := Document{}
	for key, value := range fields.All() {
		data[key] = value
	}
	if data == nil {
		return nil, failure("INVALID_RESPONSE", 502, msgResponseShape)
	}
	if response.StatusCode >= 400 || !yes(data["success"]) {
		message := text(data["message"])
		if message == "" {
			message = text(data["error"])
		}
		if message == "" {
			message = msgOperation
		}
		status := response.StatusCode
		if status != 400 && status != 409 && status != 422 {
			status = 502
		}
		message = diag.FailureText(message, []any{text(a["token"])}, diag.DetailLimit)
		return nil, failure("IPV6_OPERATION_FAILED", status, message)
	}
	return data, nil
}

func (s *Service) all(ctx context.Context) ([]Document, error) {
	records, err := s.Manager.ReadAgents(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(records))
	for i := range records {
		if !agents.FreshHeartbeat(records[i].LastHeartbeat, nil) {
			records[i].Status = "offline"
		}
		d, err := agentDocument(&records[i])
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}
func (s *Service) update(ctx context.Context, id string, fn func(context.Context, Document, []Document) error) error {
	return s.Manager.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		all := make([]Document, 0, len(*records))
		index := -1
		for i := range *records {
			a := &(*records)[i]
			d, err := agentDocument(a)
			if err != nil {
				return err
			}
			all = append(all, d)
			if agents.AgentMatchesID(a, id) {
				index = i
			}
		}
		var current Document
		if index >= 0 {
			current = all[index]
		}
		if err := fn(tx, current, all); err != nil {
			return err
		}
		if index < 0 {
			return nil
		}
		var next model.Agent
		if err := json.Unmarshal(jsonresp.Value(current), &next); err != nil {
			return err
		}
		(*records)[index] = next
		return tx.Err()
	})
}

func validURL(value string) error {
	return healthcheck.ValidateHealthJSON(jsonresp.Value(struct {
		URL string `json:"url"`
	}{value}), false)
}
func defaultURL() string { return healthcheck.DefaultGroupHealth().URL }
func (s *Service) capabilities(ctx context.Context, a Document) Document {
	o, e := agentRecord(a)
	if e != nil {
		return nil
	}
	answer, e := s.Manager.DeploymentRequestModel(ctx, o, http.MethodGet, "capabilities", nil, nil, &agents.Timeout{Connect: 2 * time.Second, Read: 3 * time.Second})
	if e != nil {
		return nil
	}
	data, e := model.Marshal(answer)
	if e != nil {
		return nil
	}
	return object(data)
}
func (s *Service) receipt(ctx context.Context, a Document) (*deploy.Receipt, error) {
	o, e := agentRecord(a)
	if e != nil {
		return nil, e
	}
	return deploy.NewReceipts(s.Manager.Repository()).LoadAgent(ctx, o, false)
}

func (s *Service) systemSecrets(ctx context.Context) (redact.Secrets, error) {
	// ctx already carries runtime ownership; repository is passed explicitly.
	settings, e := s.Manager.Repository().SystemConfig(ctx)
	if e != nil {
		return nil, e
	}
	raw, e := model.Marshal(settings)
	if e != nil {
		return nil, e
	}
	seeds, err := redact.ExternalPayloadSecrets(ctx, s.Manager.Repository(), json.RawMessage(raw))
	return redact.NewSecrets(seeds, []string{os.Getenv("AGENT_REGISTRATION_KEY")}), err
}
