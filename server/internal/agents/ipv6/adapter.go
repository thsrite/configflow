package ipv6

// Legacy repo, converter and registry types stop at this adapter. The IPv6
// owners only operate on native snapshots and independently owned Agent records.
import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/diag"
	"configflow/internal/agents/monitor"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	healthcheck "configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

func defaultMihomo() (*yamlx.Node, error) { return yamlbridge.Encode(convert.DefaultMihomoTemplate()) }

// Registry and repository compatibility boundaries.
func legacy(d Document) (*jsonx.Object, error) {
	v, e := jsonx.Parse(raw(d))
	if e != nil {
		return nil, e
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("ipv6: expected object")
	}
	return o, nil
}
func native(o *jsonx.Object) Document {
	if o == nil {
		return nil
	}
	b, _ := jsonx.Compact(o)
	return object(b)
}
func (s *Service) snapshot(ctx context.Context, profile string) (Document, int64, error) {
	o, r, e := s.Manager.Repository().GetPublishSnapshot(ctx, profile, true)
	return native(o), r, e
}
func (s *Service) agent(ctx context.Context, id, profile string) (Document, error) {
	a, e := s.Manager.GetAgentByID(ctx, id, true)
	if e != nil {
		return nil, e
	}
	d := native(a)
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
	a, e := s.Manager.GetAgentByID(ctx, id, true)
	if e != nil {
		return nil
	}
	return native(a)
}
func online(a Document) bool {
	o, e := legacy(a)
	return e == nil && agents.OnlineStatus(o, nil) != "offline"
}
func origin(a Document) (string, error) {
	o, e := legacy(a)
	if e != nil {
		return "", e
	}
	return monitor.AgentOrigin(o)
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
	o, e := legacy(a)
	if e != nil {
		return "unknown"
	}
	src, e := deploy.SourceFromJSON(raw(config), &revision)
	if e != nil {
		return "unknown"
	}
	var status string
	if wait {
		status = checker.CompareNow(ctx, deploy.NewTarget(o), src, text(applied["base_url"])).Status
	} else {
		status = checker.Status(ctx, deploy.NewTarget(o), src, text(applied["base_url"])).Status
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
		body = raw(payload)
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
	data := object(response.Body)
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
	rows, e := s.Manager.GetAllAgents(ctx, true)
	if e != nil {
		return nil, e
	}
	out := []Document{}
	for _, a := range rows {
		if a != nil {
			out = append(out, native(a))
		}
	}
	return out, nil
}
func (s *Service) update(ctx context.Context, id string, fn func(context.Context, Document, []Document) error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	_, e := s.Manager.UpdateAgents(ctx, "", func(ctx context.Context, rows *[]any) (any, error) {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		all := []Document{}
		index := -1
		for i, v := range *rows {
			if a, ok := v.(*jsonx.Object); ok {
				d := native(a)
				all = append(all, d)
				if text(d["id"]) == id {
					index = i
				}
			}
		}
		var current Document
		if index >= 0 {
			current = native((*rows)[index].(*jsonx.Object))
		}
		if e := fn(ctx, current, all); e != nil {
			return nil, e
		}
		if index < 0 {
			return nil, nil
		}
		next, e := legacy(current)
		if e != nil {
			return nil, e
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		(*rows)[index] = next
		return nil, nil
	})
	return e
}
func validURL(value string) error { return healthcheck.ValidateHealth(jsonx.Obj("url", value), false) }
func defaultURL() string          { return healthcheck.GroupDefaults().GetString("url") }
func (s *Service) capabilities(ctx context.Context, a Document) Document {
	o, e := legacy(a)
	if e != nil {
		return nil
	}
	answer, e := s.Manager.DeploymentRequest(ctx, o, http.MethodGet, "capabilities", nil, nil, &agents.Timeout{Connect: 2 * time.Second, Read: 3 * time.Second})
	if e != nil {
		return nil
	}
	return native(answer)
}
func (s *Service) receipt(ctx context.Context, a Document) (*deploy.Receipt, error) {
	o, e := legacy(a)
	if e != nil {
		return nil, e
	}
	return deploy.NewReceipts(s.Manager.Repository().DataDir()).Load(ctx, o, false)
}

func (s *Service) systemSecrets(ctx context.Context) (redact.Secrets, error) {
	ctx = repo.WithRepository(ctx, s.Manager.Repository())
	system, e := repo.GetSystemConfig(ctx)
	if e != nil {
		return nil, e
	}
	seeds, err := redact.ExternalPayloadSecrets(ctx, system)
	return redact.NewSecrets(seeds, []string{os.Getenv("AGENT_REGISTRATION_KEY")}), err
}
