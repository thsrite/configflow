package installation

// Legacy repository/registry boundaries are confined to this file. The
// installation state machine itself uses native structs and raw JSON.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/convert"
	"configflow/internal/deploy"
	"configflow/internal/gateway"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/installer"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
)

type agent struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Host             string                     `json:"host"`
	Port             int                        `json:"port"`
	Token            string                     `json:"token"`
	ProfileID        string                     `json:"profile_id"`
	ServiceType      string                     `json:"service_type"`
	DeploymentMethod string                     `json:"deployment_method"`
	InstallationID   string                     `json:"gateway_installation_id"`
	Latest           task                       `json:"latest_deployment"`
	Deployments      map[string]json.RawMessage `json:"deployments"`
	record           *model.Agent
	usable           bool
	public           json.RawMessage
}

func decodeAgent(r *model.Agent) (*agent, error) {
	if r == nil {
		return nil, nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	o := object(b)
	a := agentFields(o)
	a.record = r
	p := map[string]json.RawMessage{}
	for _, key := range []string{"id", "name", "host", "port", "profile_id", "service_type", "enabled", "version"} {
		p[key] = o[key]
		if len(p[key]) == 0 {
			p[key] = raw(nil)
		}
	}
	now := time.Now()
	status := o["status"]
	if !agents.FreshHeartbeat(r.LastHeartbeat, &now) {
		status = raw("offline")
	}
	if len(status) == 0 {
		status = raw(nil)
	}
	p["status"] = status
	a.public = raw(p)
	a.usable = string(o["enabled"]) != "false" && text(status) == "online"
	return &a, nil
}

// agentFields reads the identity members Python only compares for equality.
// A member of another JSON type (an Agent edited to a string port, a legacy
// record) never matches an installation target, and it must not turn every
// registration or status read into a server error.
func agentFields(o map[string]json.RawMessage) agent {
	var latest task
	if l := object(o["latest_deployment"]); l != nil {
		latest = task{DeploymentID: text(l["deployment_id"]), Status: text(l["status"])}
	}
	deployments := object(o["deployments"])
	return agent{ID: text(o["id"]), Name: text(o["name"]), Host: text(o["host"]), Port: port(o["port"]), Token: text(o["token"]), ProfileID: text(o["profile_id"]), ServiceType: text(o["service_type"]), DeploymentMethod: text(o["deployment_method"]), InstallationID: text(o["gateway_installation_id"]), Latest: latest, Deployments: deployments}
}

// port is an integral JSON number (Python 8080.0 == 8080), otherwise -1.
func port(b json.RawMessage) int {
	var f float64
	if json.Unmarshal(b, &f) != nil || f != math.Trunc(f) || f < 0 || f > 65535 {
		return -1
	}
	return int(f)
}
func (s *Service) agent(ctx context.Context, id string) (*agent, error) {
	r, err := s.Manager.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	return decodeAgent(r)
}
func (s *Service) allAgents(ctx context.Context) ([]*agent, error) {
	rr, err := s.Manager.ReadAgents(ctx, true, nil)
	if err != nil {
		return nil, err
	}
	out := make([]*agent, 0, len(rr))
	for i := range rr {
		a, e := decodeAgent(&rr[i])
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func validateProfile(profile string) error { return repo.ValidateProfileID(profile) }
func (s *Service) checkPorts(ctx context.Context, profile string, ports map[string]int) error {
	p, err := s.Repository.Profile(ctx, profile)
	if err != nil {
		return err
	}
	text := p.Mihomo.CustomConfig
	document := convert.MihomoTemplate()
	if strings.TrimSpace(text) != "" {
		n, e := yamlx.Parse([]byte(text))
		if e != nil {
			return nil
		}
		document = n
	}
	if document == nil {
		return nil
	}
	owners := convert.ListenPorts(document)
	for _, service := range services {
		for _, o := range owners {
			if o.Port == int64(ports[service]) {
				return failure(fmt.Sprintf(msgPortConflict, service, ports[service], o.Owner), 400)
			}
		}
	}
	return nil
}
func (s *Service) setDomain(ctx context.Context, origin string) error {
	err := s.Repository.UpdateSystem(ctx, func(tx context.Context, current *model.System) error {
		if err := tx.Err(); err != nil {
			return err
		}
		current.SystemConfig.ServerDomain = origin
		return tx.Err()
	})
	return err
}
func (s *Service) savedSnapshot(ctx context.Context, r *Record) (*deploy.Source, string, error) {
	snapshot, revision, err := s.Repository.PublishConfig(ctx, r.ProfileID, true)
	if err != nil {
		return nil, "", err
	}
	if revision != r.ConfigRevision {
		return nil, "", failure(msgSavedChanged, 409)
	}
	mode := "selective"
	if takeoverFullTun(snapshot.Profile) {
		mode = "full_tun"
	}
	if r.Takeover != "" && r.Takeover != mode {
		return nil, "", failure(msgTakeoverChanged, 409)
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", err
	}
	var config onboarding.Document
	if err = json.Unmarshal(b, &config); err != nil {
		return nil, "", err
	}
	options := onboarding.Options{BaseURL: r.ServerURL}
	if a, e := netip.ParseAddr(r.Network.Address); e == nil {
		options.LocalAddresses = []netip.Addr{a}
	}
	if _, err = onboarding.AssertReady(ctx, config, options); err != nil {
		return nil, "", s.safeError(ctx, publicOwnerError(err))
	}
	configured := strings.TrimRight(strings.TrimSpace(snapshot.SystemConfig.ServerDomain), "/")
	if configured != "" && configured != r.ServerURL {
		return nil, "", &Error{Message: fmt.Sprintf(msgDomainMismatch, configured, r.ServerURL), Status: 409, Code: "server_url_mismatch"}
	}
	source, err := deploy.SourceFromJSON(b, &revision)
	return source, mode, err
}
func publicOwnerError(err error) error {
	if err == nil {
		return nil
	}
	var m *monitor.Error
	if errors.As(err, &m) {
		return &Error{Message: m.Message, Status: m.Status, Cause: err}
	}
	var iv *installer.ValueError
	if errors.As(err, &iv) {
		return &Error{Message: iv.Error(), Status: 400, Cause: err}
	}
	var g *gateway.Error
	if errors.As(err, &g) {
		return &Error{Message: g.Error(), Status: 400, Cause: err}
	}
	return err
}
func (s *Service) secrets(ctx context.Context) (redact.Secrets, error) {
	ctx = repo.WithRepository(ctx, s.Repository)
	settingsModel, err := s.Repository.SystemConfig(ctx)
	if err != nil {
		return nil, err
	}
	settings, err := settingsModel.MarshalJSON()
	if err != nil {
		return nil, err
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, json.RawMessage(settings))
	if err != nil {
		return nil, err
	}
	return redact.CollectCredentials(nil, secrets), nil
}
func (s *Service) diagnosticSecrets(ctx context.Context) (redact.Secrets, error) {
	seeds, err := s.secrets(ctx)
	if err != nil {
		return nil, err
	}
	document, err := s.Repository.ExportAllJSON(ctx, false)
	if err != nil {
		return nil, err
	}
	return redact.CollectCredentials(document, seeds), nil
}
func (s *Service) safeError(ctx context.Context, err error) error {
	if err == nil || ctx.Err() != nil {
		return err
	}
	var e *Error
	if !errors.As(err, &e) {
		return err
	}
	secrets, se := s.diagnosticSecrets(ctx)
	if se != nil {
		return se
	}
	safe := text(redact.SanitizeCredentials(e.Message, secrets))
	cp := *e
	cp.Message = safe
	return &cp
}
func encodeResult(result *model.Deployment) (json.RawMessage, error) { return json.Marshal(result) }

func (s *Service) bind(ctx context.Context, r *Record, proofs map[string]json.RawMessage) (map[string]string, error) {
	ids := map[string]string{}
	err := s.Manager.EditAgents(ctx, r.ProfileID, func(ctx context.Context, records *[]model.Agent) error {
		type binding struct {
			record  *model.Agent
			service string
		}
		found := []binding{}
		index := map[string]*model.Agent{}
		// Python refuses when any other Agent holds the target, so keep every
		// holder: a last-wins index misses a duplicate listed before the proof.
		targets := map[[2]string][]string{}
		for i := range *records {
			a := &(*records)[i]
			{
				index[strictAgent(a).ID] = a
				key := [2]string{strictAgent(a).Name, strictAgent(a).Host}
				targets[key] = append(targets[key], strictAgent(a).ID)
			}
		}
		for _, service := range services {
			proof := object(proofs[service])
			if len(proof) != 2 || proof["id"] == nil || proof["token"] == nil {
				return failure(msgProof, 401)
			}
			var id, token string
			if json.Unmarshal(proof["id"], &id) != nil || json.Unmarshal(proof["token"], &token) != nil || id == "" || token == "" {
				return failure(msgProof, 401)
			}
			a := index[id]
			if a == nil || !agents.ConstantTimeASCIIEqual(token, strictAgent(a).Token) {
				return failure(msgProofFailed, 401)
			}
			if strictAgent(a).ServiceType != service || strictAgent(a).DeploymentMethod != "shell" {
				return failure(msgShell, 409)
			}
			if bound := r.AgentIDs[service]; bound != "" && bound != id {
				return failure(msgBound, 409)
			}
			if registered := r.Registered[service]; registered != "" && registered != id {
				return failure(msgRegistration, 409)
			}
			expectedName := r.Name + "-" + service
			changed := strictAgent(a).Name != expectedName || strictAgent(a).Host != r.Network.Address || strictAgent(a).Port != r.Ports[service] || strictAgent(a).ProfileID != r.ProfileID
			if r.AgentIDs[service] != "" && changed {
				return failure(msgBoundMetadata, 409)
			}
			for _, other := range targets[[2]string{expectedName, r.Network.Address}] {
				if other != id {
					return failure(msgDuplicate, 409)
				}
			}
			if changed {
				busy := agents.UpgradeInProgress(a) || agents.DNSFollowInProgress(*records, id)
				for id, d := range a.Deployments.All() {
					if a.Deployments.EntryValid(id) && d.Status != "succeeded" && d.Status != "failed" && d.Status != "rolled_back" {
						busy = true
					}
				}
				if busy {
					return failure(msgBusy, 409)
				}
			}
			ids[service] = id
			found = append(found, binding{a, service})
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, b := range found {
			a := b.record
			a.Name = r.Name + "-" + b.service
			a.Host = r.Network.Address
			a.Port = model.Int(r.Ports[b.service])
			a.ProfileID = r.ProfileID
		}
		return ctx.Err()
	})
	return ids, err
}

func (s *Service) safeText(ctx context.Context, value string) (string, error) {
	secrets, err := s.diagnosticSecrets(ctx)
	if err != nil {
		return "", err
	}
	return text(redact.SanitizeCredentials(value, secrets)), nil
}
func (s *Service) safeMetadata(ctx context.Context, value json.RawMessage) (json.RawMessage, error) {
	system, err := s.secrets(ctx)
	if err != nil {
		return nil, err
	}
	diagnostics, err := s.diagnosticSecrets(ctx)
	if err != nil {
		return nil, err
	}
	fields := object(value)
	if fields == nil {
		return nil, failure(msgCorrupt, 409)
	}
	for _, key := range []string{"message", "error", "rollback_error"} {
		if v, ok := fields[key]; ok {
			fields[key] = redact.SanitizeCredentials(v, diagnostics)
		}
	}
	return json.Marshal(redact.SanitizeWithSecrets(fields, system, true))
}
func (s *Service) deployment(ctx context.Context, id, did string, activate bool) (json.RawMessage, error) {
	var result *model.Deployment
	var err error
	if activate {
		result, err = s.Manager.ActivateDeploymentModel(ctx, id, did)
	} else {
		result, err = s.Manager.GetDeploymentModel(ctx, id, did)
	}
	if err != nil {
		return nil, err
	}
	return encodeResult(result)
}
func (s *Service) recordFailed(ctx context.Context, id, did string) (json.RawMessage, error) {
	update, err := agents.DecodeDeploymentUpdate(raw(map[string]any{"success": false, "status": "failed", "message": msgPrepareFailed}))
	if err != nil {
		return nil, err
	}
	result, err := s.Manager.RecordDeploymentModel(ctx, id, did, update, agents.RecordOptions{})
	if err != nil {
		return nil, err
	}
	return encodeResult(result)
}

func (s *Service) safeMetadataText(ctx context.Context, value json.RawMessage) (json.RawMessage, error) {
	secrets, err := s.diagnosticSecrets(ctx)
	if err != nil {
		return nil, err
	}
	return redact.SanitizeCredentials(value, secrets), nil
}

func strictAgent(a *model.Agent) agent {
	if a == nil {
		return agent{}
	}
	data, _ := model.Marshal(a)
	return agentFields(object(data))
}
func takeoverFullTun(p *model.Profile) bool {
	state, _ := routersplit.ReadMarkerProfile(p)
	return state == routersplit.StateValid
}
