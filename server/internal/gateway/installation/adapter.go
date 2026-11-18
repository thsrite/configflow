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
	"configflow/internal/jsonx"
	"configflow/internal/redact"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/yamlbridge"
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
	record           *agents.Record
	usable           bool
	public           json.RawMessage
}

func decodeAgent(r *agents.Record) (*agent, error) {
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
	p["status"] = raw(agents.OnlineStatus(r, &now))
	a.public = raw(p)
	a.usable = agents.IsUsable(r, &now)
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
	r, err := s.Manager.GetAgentByID(ctx, id, true)
	if err != nil {
		return nil, err
	}
	return decodeAgent(r)
}
func (s *Service) allAgents(ctx context.Context) ([]*agent, error) {
	rr, err := s.Manager.Agents(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]*agent, 0, len(rr))
	for _, r := range rr {
		a, e := decodeAgent(r)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, nil
}
func validateProfile(profile string) error { return repo.ValidateProfileID(profile) }
func (s *Service) checkPorts(ctx context.Context, profile string, ports map[string]int) error {
	p, err := s.Repository.GetProfile(ctx, profile)
	if err != nil {
		return err
	}
	text := p.GetObject("mihomo").GetString("custom_config")
	document := convert.DefaultMihomoTemplate()
	if strings.TrimSpace(text) != "" {
		n, e := yamlx.Parse([]byte(text))
		if e != nil {
			return nil
		}
		v, e := yamlbridge.Value(n)
		if e != nil {
			return e
		}
		document, _ = v.(*jsonx.Object)
	}
	if document == nil {
		return nil
	}
	owners, err := convert.MihomoListenPorts(document)
	if err != nil {
		return err
	}
	for _, service := range services {
		for _, o := range owners {
			if o.Port.String() == fmt.Sprint(ports[service]) {
				return failure(fmt.Sprintf(msgPortConflict, service, ports[service], o.Owner), 400)
			}
		}
	}
	return nil
}
func (s *Service) setDomain(ctx context.Context, origin string) error {
	_, err := s.Repository.UpdateSystemTransaction(ctx, func(ctx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sc := current.GetObject("system_config")
		if sc == nil {
			sc = jsonx.NewObject()
			current.Set("system_config", sc)
		}
		sc.Set("server_domain", origin)
		return nil, nil
	})
	return err
}
func (s *Service) savedSnapshot(ctx context.Context, r *Record) (*deploy.Source, string, error) {
	snapshot, revision, err := s.Repository.GetPublishSnapshot(ctx, r.ProfileID, true)
	if err != nil {
		return nil, "", err
	}
	if revision != r.ConfigRevision {
		return nil, "", failure(msgSavedChanged, 409)
	}
	mode := "selective"
	if routersplit.IsFullTun(snapshot) {
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
	configured := strings.TrimRight(strings.TrimSpace(snapshot.GetObject("system_config").GetString("server_domain")), "/")
	if configured != "" && configured != r.ServerURL {
		return nil, "", &Error{Message: fmt.Sprintf(msgDomainMismatch, configured, r.ServerURL), Status: 409, Code: "server_url_mismatch"}
	}
	return deploy.NewSource(snapshot, &revision), mode, nil
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
	system, err := s.Repository.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	secrets, err := redact.ExternalPayloadSecrets(ctx, system.GetObject("system_config"))
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
	document, err := s.Repository.ExportAll(ctx, false)
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
func toLegacy(data json.RawMessage) (*agents.Record, error) {
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	o, ok := v.(*jsonx.Object)
	if !ok {
		return nil, failure(msgParams, 400)
	}
	return o, nil
}
func encodeResult(result *agents.Result) (json.RawMessage, error) { return json.Marshal(result) }

func (s *Service) bind(ctx context.Context, r *Record, proofs map[string]json.RawMessage) (map[string]string, error) {
	ids := map[string]string{}
	_, err := s.Manager.UpdateAgents(ctx, r.ProfileID, func(ctx context.Context, records *[]any) (any, error) {
		type binding struct {
			record  *jsonx.Object
			service string
		}
		found := []binding{}
		index := map[string]*jsonx.Object{}
		// Python refuses when any other Agent holds the target, so keep every
		// holder: a last-wins index misses a duplicate listed before the proof.
		targets := map[[2]string][]string{}
		for _, item := range *records {
			a, _ := item.(*jsonx.Object)
			if a != nil {
				index[a.GetString("id")] = a
				key := [2]string{a.GetString("name"), a.GetString("host")}
				targets[key] = append(targets[key], a.GetString("id"))
			}
		}
		for _, service := range services {
			proof := object(proofs[service])
			if len(proof) != 2 || proof["id"] == nil || proof["token"] == nil {
				return nil, failure(msgProof, 401)
			}
			var id, token string
			if json.Unmarshal(proof["id"], &id) != nil || json.Unmarshal(proof["token"], &token) != nil || id == "" || token == "" {
				return nil, failure(msgProof, 401)
			}
			a := index[id]
			if a == nil || !agents.ConstantTimeASCIIEqual(token, a.GetString("token")) {
				return nil, failure(msgProofFailed, 401)
			}
			if a.GetString("service_type") != service || a.GetString("deployment_method") != "shell" {
				return nil, failure(msgShell, 409)
			}
			if bound := r.AgentIDs[service]; bound != "" && bound != id {
				return nil, failure(msgBound, 409)
			}
			if registered := r.Registered[service]; registered != "" && registered != id {
				return nil, failure(msgRegistration, 409)
			}
			expectedName := r.Name + "-" + service
			changed := a.GetString("name") != expectedName || a.GetString("host") != r.Network.Address || !jsonx.Equal(a.Value("port"), jsonx.Int(int64(r.Ports[service]))) || a.GetString("profile_id") != r.ProfileID
			if r.AgentIDs[service] != "" && changed {
				return nil, failure(msgBoundMetadata, 409)
			}
			for _, other := range targets[[2]string{expectedName, r.Network.Address}] {
				if other != id {
					return nil, failure(msgDuplicate, 409)
				}
			}
			if changed {
				busy := agents.UpgradeBusy(a) || agents.DNSFollowBusy(*records, id)
				for _, value := range a.GetObject("deployments").All() {
					d, _ := value.(*jsonx.Object)
					status := d.GetString("status")
					if status != "succeeded" && status != "failed" && status != "rolled_back" {
						busy = true
					}
				}
				if busy {
					return nil, failure(msgBusy, 409)
				}
			}
			ids[service] = id
			found = append(found, binding{a, service})
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, b := range found {
			a := b.record
			a.Set("name", r.Name+"-"+b.service)
			a.Set("host", r.Network.Address)
			a.Set("port", jsonx.Int(int64(r.Ports[b.service])))
			a.Set("profile_id", r.ProfileID)
		}
		return nil, nil
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
	var result *agents.Result
	var err error
	if activate {
		result, err = s.Manager.ActivateDeployment(ctx, id, did)
	} else {
		result, err = s.Manager.GetDeployment(ctx, id, did)
	}
	if err != nil {
		return nil, err
	}
	return encodeResult(result)
}
func (s *Service) recordFailed(ctx context.Context, id, did string) (json.RawMessage, error) {
	result, err := s.Manager.RecordDeployment(ctx, id, did, jsonx.Obj("success", false, "status", "failed", "message", msgPrepareFailed), agents.RecordOptions{})
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
