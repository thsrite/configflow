package deploy

// All legacy document boundaries live here; the rest of deploy only sees typed
// values, yamlx nodes, opaque records or raw JSON.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/redact"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
)

// Source is an immutable profile snapshot until the typed repository lands.
type Source struct {
	legacy    *jsonx.Object
	revision  *int64
	inputOnce sync.Once
	inputKey  InputKey
	inputErr  error
}

// NewSource captures a legacy profile and its optional revision.
func NewSource(config *jsonx.Object, revision *int64) *Source {
	s := &Source{}
	if config != nil {
		s.legacy = config.DeepCopy()
	}
	if revision != nil {
		r := *revision
		s.revision = &r
	}
	return s
}

// ProfileID returns the snapshot's resolved profile identifier.
func (s *Source) ProfileID() string {
	if s == nil {
		return ""
	}
	return s.legacy.GetString("profile_id")
}
func (s *Source) gateway() *gateway.Config {
	if s == nil {
		return gateway.NewConfig(nil)
	}
	return gateway.NewConfig(s.legacy)
}

// NewTarget captures the Agent fields needed by the publishing chain.
func NewTarget(record *jsonx.Object) Target {
	if record == nil {
		return Target{}
	}
	r := record.DeepCopy()
	port, _ := json.Marshal(r.Value("port"))
	return Target{ID: r.GetString("id"), Name: r.GetString("name"), ProfileID: r.GetString("profile_id"), ServiceType: r.GetString("service_type"), DeploymentMethod: r.GetString("deployment_method"), Version: r.GetString("version"), Host: r.GetString("host"), Port: port, Token: r.GetString("token"), DomainDiscovery: r.GetBool("domain_discovery_enabled"), Record: r}
}

func (s *Source) config() *jsonx.Object {
	if s == nil || s.legacy == nil {
		return jsonx.NewObject()
	}
	return s.legacy
}
func (s *Source) serverDomain(fallback string) string {
	domain := s.config().GetObject("system_config").GetString("server_domain")
	if domain == "" {
		return fallback
	}
	return domain
}
func (s *Source) internal(rawURL, baseURL string) bool {
	cfg := s.config().DeepCopy()
	settings := cfg.GetObject("system_config")
	if settings == nil {
		settings = jsonx.NewObject()
		cfg.Set("system_config", settings)
	}
	settings.Set("server_domain", s.serverDomain(baseURL))
	return rulefetch.IsInternalRuleURL(rawURL, cfg, "")
}
func (s *Source) profilePath(suffix string) string {
	if id := s.ProfileID(); id != "" {
		return "/api/profiles/" + id + suffix
	}
	return suffix
}
func (s *Source) library() []RuleRef {
	out := []RuleRef{}
	for _, value := range s.config().GetList("rule_library") {
		item, ok := value.(*jsonx.Object)
		if !ok {
			continue
		}
		out = append(out, RuleRef{ID: item.GetString("id"), Name: item.GetString("name"), URL: item.GetString("url"), SourceType: item.GetString("source_type"), Content: item.GetString("content"), Behavior: item.GetString("behavior"), Format: item.GetString("format")})
	}
	return out
}
func (s *Source) requestRule(ctx context.Context, rawURL string) (*rulefetch.Response, error) {
	return rulefetch.RequestRule(ctx, rawURL, rulefetch.Request{Config: s.config(), MaxBytes: MaxFileBytes, Stream: true, ConnectTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second})
}

// ruleRead is a declared remote source read through the shared refresh cache.
type ruleRead struct {
	data     []byte
	stale    error  // the failed refresh when the last good cache was used
	name     string // the declaration's name, for the stale-cache warning
	declared bool   // false: no URL declaration, fetch the URL directly
}

func (s *Source) readRule(ctx context.Context, rawURL, sourceID string, declared *RuleRef, fetch func(context.Context, string) ([]byte, error)) (ruleRead, error) {
	cfg := s.config()
	rule, err := rulefetch.ResolveRemoteRuleSource(rawURL, cfg, sourceID)
	if err != nil {
		return ruleRead{declared: true}, err
	}
	if rule == nil && declared != nil {
		rule = jsonx.Obj("name", declared.Name, "behavior", declared.Behavior, "format", declared.Format, "source_type", "url", "url", rawURL)
	}
	if rule == nil || rule.GetOr("source_type", "url") != "url" {
		return ruleRead{}, nil
	}
	value, stale, err := refresh.ReadRuleSource(ctx, rule, refresh.RuleOptions{Config: cfg, Fetch: func(u string) (any, error) { return fetch(ctx, u) }})
	return ruleRead{data: value, stale: stale, name: rule.GetString("name"), declared: true}, err
}

// SourceError renders the provider's native error and one-based positions.
func (Locations) SourceError(message, text string, line, column *int, cause error, sourceLabel string) string {
	var syntax *yamlx.SyntaxError
	if line == nil && errors.As(cause, &syntax) && syntax.Line > 0 {
		l, c := syntax.Line, max(1, syntax.Column)
		line, column = &l, &c
	}
	diagnostic := ""
	if cause != nil {
		diagnostic = cause.Error()
	}
	return sourceDiagnostic(message, text, Position{Line: line, Column: column}, cause, sourceLabel, diagnostic).Error()
}

func downloadRef(d convert.Download) (RuleRef, string, error) {
	u, ok := d.URL.(string)
	if !ok {
		return RuleRef{}, "", preparation("Required rule has no source URL")
	}
	p, ok := d.LocalPath.(string)
	if !ok {
		return RuleRef{}, "", preparation("Invalid deployment file path")
	}
	return RuleRef{Name: d.Name, URL: u, Behavior: d.Behavior, Format: d.Format}, p, nil
}

// Wire installs deployment preparation diagnostics. A14 adds publication hooks.
func Wire() {
	provider.SetLocations(Locations{})
	agents.RegisterHooks(agents.Hooks{ReceiptMatches: ReceiptMatches})
}

func (s *Source) normalize(value []byte, rule RuleRef) ([]byte, error) {
	r := jsonx.Obj("id", rule.ID, "name", rule.Name, "url", rule.URL, "source_type", rule.SourceType, "behavior", defaultBehavior(rule.Behavior), "format", rule.Format)
	format, err := convert.RulesetFormat(r, s.config(), "")
	if err != nil {
		return nil, err
	}
	text, warnings, err := rulefetch.NormalizeMihomoRuleset(string(value), defaultBehavior(rule.Behavior), format)
	if len(warnings) > 0 {
		logger.Warningf("Mihomo provider %s: %d unconverted lines", rule.Name, len(warnings))
	}
	return []byte(text), err
}
func (s *Source) validateRemote(value []byte, rule RuleRef) error {
	return rulefetch.ValidateRemoteRuleContent(value, jsonx.Obj("name", rule.Name, "url", rule.URL, "source_type", "url", "behavior", rule.Behavior, "format", rule.Format), jsonx.NewObject(), rule.Format)
}
func (s *Source) unavailable(paths []string) []string {
	return oxidns.UnavailableRulesets(oxidns.NewProfile(s.config()), paths)
}
func resolvedBuildProfile(ctx context.Context, src *Source, t Target) (string, error) {
	if id := src.ProfileID(); id != "" {
		return id, nil
	}
	if t.ProfileID != "" {
		return t.ProfileID, nil
	}
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return "", err
	}
	return r.DefaultProfileID(ctx)
}

func (s *Source) snapshotJSON() (json.RawMessage, error) {
	cfg := s.config().DeepCopy()
	cfg.Delete("agents")
	return encodeJSON(cfg)
}
func (s *Source) inputDigest() string { return s.contentDigest() }

func nativeCanonical(raw []byte) ([]byte, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return encodeJSON(value)
}
func equalJSON(a, b []byte) bool {
	left, err := yamlx.FromJSON(a)
	if err != nil {
		return false
	}
	right, err := yamlx.FromJSON(b)
	return err == nil && yamlx.Equal(left, right)
}

type appliedView struct {
	PreparedContent
	DeploymentID  string `json:"deployment_id"`
	ConfigVersion string `json:"config_version"`
}
type receiptAgentView struct {
	ID            string      `json:"id"`
	ProfileID     string      `json:"profile_id"`
	ServiceType   string      `json:"service_type"`
	ConfigVersion string      `json:"config_version"`
	Applied       appliedView `json:"applied_content"`
}

func receiptAgentRaw(agent *agents.Record) receiptAgentView {
	var view receiptAgentView
	raw, err := encodeJSON(agent)
	if err == nil {
		_ = json.Unmarshal(raw, &view)
	}
	return view
}
func receiptAgent(ctx context.Context, agent *agents.Record) (receiptAgentView, error) {
	view := receiptAgentRaw(agent)
	if view.ProfileID == "" {
		r, err := repo.GetRepository(ctx)
		if err != nil {
			return view, err
		}
		view.ProfileID, err = r.DefaultProfileID(ctx)
		if err != nil {
			return view, err
		}
	}
	return view, nil
}
func retainedReceipts(ctx context.Context, r *repo.Repository, agentID, deploymentID string) (map[string]bool, error) {
	system, err := r.GetSystem(ctx)
	if err != nil {
		return nil, err
	}
	retained := map[string]bool{digest([]byte(deploymentID)): true, digest(nil): true}
	for _, value := range system.GetList("agents") {
		agent, ok := value.(*jsonx.Object)
		if !ok || agent.GetString("id") != agentID {
			continue
		}
		if id := agent.GetObject("applied_content").GetString("deployment_id"); id != "" {
			retained[digest([]byte(id))] = true
		}
		tasks := agent.GetObject("deployments")
		if tasks != nil {
			for id, value := range tasks.All() {
				state, ok := value.(*jsonx.Object)
				if ok && !finished(state.GetString("status")) {
					retained[digest([]byte(id))] = true
				}
			}
		}
		break
	}
	return retained, nil
}
func finished(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rolled_back"
}
func advanceApplied(ctx context.Context, m *agents.Manager, agent *agents.Record, fingerprint string, files map[string]string) (bool, error) {
	accepted := false
	expected := agent.Value("applied_content")
	id := agent.GetString("id")
	if id == "" || expected == nil {
		return false, nil
	}
	fileMap := jsonx.NewObject()
	for p, sha := range files {
		fileMap.Set(p, sha)
	}
	_, err := m.Repository().UpdateSystemTransaction(ctx, func(ctx context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		for _, value := range system.GetList("agents") {
			current, ok := value.(*jsonx.Object)
			if !ok || current.GetString("id") != id {
				continue
			}
			applied := current.GetObject("applied_content")
			if applied == nil || !jsonx.Equal(applied, expected) {
				return nil, nil
			}
			latest := current.GetObject("latest_deployment")
			if latest != nil && !finished(latest.GetString("status")) {
				return nil, nil
			}
			applied.Set("fingerprint", fingerprint)
			applied.Set("files", fileMap.DeepCopy())
			deploymentID := applied.GetString("deployment_id")
			for _, state := range []*jsonx.Object{current.GetObject("deployments").GetObject(deploymentID), latest} {
				if state != nil && state.GetString("deployment_id") == deploymentID {
					if prepared := state.GetObject("prepared_content"); prepared != nil {
						prepared.Set("fingerprint", fingerprint)
						prepared.Set("files", fileMap.DeepCopy())
					}
				}
			}
			accepted = true
			break
		}
		return nil, nil
	})
	return accepted, err
}
func receiptMatches(agent, state *agents.Record) bool {
	if agent == nil || state == nil || agent.GetString("id") == "" || agent.GetString("service_type") != "oxidns" || state.GetString("service_type") != "oxidns" {
		return false
	}
	view, err := receiptAgent(context.Background(), agent)
	if err != nil {
		return false
	}
	profile := state.GetString("profile_id")
	sha := state.GetString("sha256")
	fingerprint := state.GetObject("prepared_content").GetString("fingerprint")
	id := state.GetString("deployment_id")
	if profile == "" || profile != view.ProfileID || id == "" || sha == "" || fingerprint == "" {
		return false
	}
	expected := ValidationReceipt{Version: 1, RulesSHA256: RulesFingerprint(), AgentID: view.ID, DeploymentID: id, ProfileID: profile, ServiceType: "oxidns", SHA256: sha, Fingerprint: fingerprint}
	raw, err := encodeJSON(expected)
	if err != nil {
		return false
	}
	proof, err := jsonx.Parse(raw)
	return err == nil && jsonx.Equal(proof, state.Value("oxidns_validation_receipt"))
}
func publicDeployment(record *agents.Record) *agents.Record {
	if record == nil {
		return nil
	}
	out := record.DeepCopy()
	if public := out.GetObject("ha_publication"); public != nil && public.Len() > 0 {
		out = agents.HAAggregate(out, public)
	}
	out.Delete("prepared_content")
	out.Delete("oxidns_validation_receipt")
	return redact.SanitizeCredentials(agents.PublicDeploymentMessages(out), nil)
}

// AgentIdentity freezes fields that affect target adaptation, including the
// reported Mihomo core. Canonical JSON is a transient native identity.
func AgentIdentity(t Target) []byte {
	record := t.Record
	if record == nil {
		record = jsonx.Obj("id", t.ID, "profile_id", t.ProfileID, "service_type", t.ServiceType, "host", t.Host, "token", t.Token, "version", t.Version, "deployment_method", t.DeploymentMethod, "domain_discovery_enabled", t.DomainDiscovery)
		if len(t.Port) > 0 {
			if port, err := jsonx.Parse(t.Port); err == nil {
				record.Set("port", port)
			}
		}
	}
	identity := jsonx.NewObject()
	for _, key := range []string{"id", "profile_id", "service_type", "host", "port", "token", "enabled", "version", "deployment_method", "domain_discovery_enabled", "oxidns_ha"} {
		identity.Set(key, record.Value(key))
	}
	core := ""
	if record.GetBool("domain_discovery_enabled") {
		if store, err := trafficstore.Get(context.Background()); err == nil {
			core = subs.ReportedCoreVersion(record, store.StatusOf)
		}
	}
	identity.Set("core_version", core)
	raw, err := encodeJSON(identity)
	if err != nil {
		return nil
	}
	canonical, err := nativeCanonical(raw)
	if err != nil {
		return nil
	}
	return canonical
}
func previewSnapshot(ctx context.Context, m *agents.Manager, profileID string) (*Source, int64, error) {
	config, revision, err := m.Repository().GetPublishSnapshot(ctx, profileID, false)
	if err != nil {
		return nil, 0, err
	}
	return NewSource(config, &revision), revision, nil
}
func previewTarget(ctx context.Context, m *agents.Manager, id string) (Target, bool, error) {
	agent, err := m.GetAgentByID(ctx, id, false)
	if err != nil || agent == nil {
		return Target{}, false, err
	}
	return NewTarget(agent), true, nil
}
func queryCapabilities(ctx context.Context, m *agents.Manager, t Target) (Capabilities, error) {
	timeout := agents.Seconds(5)
	response, err := m.DeploymentRequest(ctx, t.Record, "GET", "capabilities", nil, nil, &timeout)
	if err != nil {
		return Capabilities{}, err
	}
	raw, err := encodeJSON(response)
	if err != nil {
		return Capabilities{}, err
	}
	var c Capabilities
	if err = json.Unmarshal(raw, &c); err != nil {
		return c, err
	}
	return c, nil
}
func finalPreview(ctx context.Context, m *agents.Manager, t Target, content string) (string, error) {
	body, err := encodeJSON(struct {
		Content string `json:"content"`
	}{content})
	if err != nil {
		return "", err
	}
	timeout := agents.Seconds(30)
	response, err := m.DeploymentRequest(ctx, t.Record, "POST", "config/final-preview", http.Header{"Content-Type": []string{"application/json"}}, body, &timeout)
	if err != nil {
		return "", err
	}
	value, ok := response.Value("content").(string)
	success, successOK := response.Value("success").(bool)
	if !successOK || !success || !ok {
		message := response.GetString("message")
		if message == "" {
			message = msgFinalPreviewFailed
		}
		return "", preparation(message)
	}
	return value, nil
}

func (s *Source) githubSource(rawURL string) string {
	candidates, err := rulefetch.GithubDownloadCandidates(rawURL, s.config(), "")
	if err == nil && len(candidates) > 0 {
		return candidates[0].URL
	}
	return rawURL
}
