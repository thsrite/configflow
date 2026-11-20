package deploy

// All legacy document boundaries live here; the rest of deploy only sees typed
// values, yamlx nodes, opaque records or raw JSON.
import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/gateway"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/oxidns"
	"configflow/internal/provider"
	"configflow/internal/redact"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/setupflow"
	"configflow/internal/subs"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// Source owns an immutable raw snapshot and its shared typed model view.
type Source struct {
	members     model.Fields
	revision    *int64
	inputOnce   sync.Once
	inputKey    InputKey
	inputErr    error
	modelConfig *model.Config
}

// NewSource captures a legacy profile and its optional revision.
func NewSource(config *jsonx.Object, revision *int64) *Source {
	if config == nil {
		source := &Source{}
		if revision != nil {
			value := *revision
			source.revision = &value
		}
		return source
	}
	raw, _ := model.Marshal(config)
	source, _ := SourceFromJSON(raw, revision)
	return source
}

// ProfileID returns the snapshot's resolved profile identifier.
func (s *Source) ProfileID() string {
	if s == nil {
		return ""
	}
	var id string
	_ = json.Unmarshal(s.Member("profile_id"), &id)
	return id
}
func (s *Source) gateway() *gateway.Config {
	raw, _ := s.Raw()
	var config jsonx.Object
	_ = json.Unmarshal(raw, &config)
	return gateway.NewConfig(&config)
}

// NewTarget captures the Agent fields needed by the publishing chain.
func NewTarget(record *jsonx.Object) Target {
	if record == nil {
		return Target{}
	}
	raw, _ := model.Marshal(record)
	var agent model.Agent
	_ = json.Unmarshal(raw, &agent)
	target, _ := NewTypedTarget(&agent)
	return target
}

func (s *Source) config() *model.Config {
	if s == nil || s.modelConfig == nil {
		return &model.Config{Profile: &model.Profile{}}
	}
	return s.modelConfig
}
func (s *Source) serverDomain(fallback string) string {
	raw, _, err := s.config().SystemConfig.MemberJSON("server_domain")
	var domain string
	if err != nil || json.Unmarshal(raw, &domain) != nil || domain == "" {
		return fallback
	}
	return domain
}
func (s *Source) ruleConfig() *rulefetch.SourceConfig {
	c := s.config()
	var sets []model.RuleConfig
	if raw, present := c.Profile.Fields.Get("rule_sets"); present {
		_ = json.Unmarshal(raw, &sets)
	}
	return &rulefetch.SourceConfig{Settings: c.SystemConfig, Library: c.RuleLibrary, Rules: c.Profile.RuleConfigs, RuleSets: sets}
}
func (s *Source) internal(rawURL, baseURL string) bool {
	cfg := *s.ruleConfig()
	cfg.Settings.ServerDomain = s.serverDomain(baseURL)
	return rulefetch.InternalURLUnchecked(rawURL, &cfg, "")
}
func (s *Source) profilePath(suffix string) string {
	if id := s.ProfileID(); id != "" {
		return "/api/profiles/" + id + suffix
	}
	return suffix
}
func (s *Source) library() []RuleRef {
	out := []RuleRef{}
	for _, item := range s.config().RuleLibrary {
		out = append(out, RuleRef{ID: item.ID, Name: item.Name, URL: textValue(item.URL), SourceType: textValue(item.SourceType), Content: textValue(item.Content), Behavior: textValue(item.Behavior), Format: textValue(item.Format)})
	}
	return out
}
func (s *Source) requestRule(ctx context.Context, rawURL string) (*rulefetch.Response, error) {
	return rulefetch.Download(ctx, rawURL, rulefetch.DownloadRequest{Config: s.ruleConfig(), MaxBytes: MaxFileBytes, Stream: true, ConnectTimeout: 10 * time.Second, ReadTimeout: 60 * time.Second})
}

// ruleRead is a declared remote source read through the shared refresh cache.
type ruleRead struct {
	data     []byte
	stale    error  // the failed refresh when the last good cache was used
	name     string // the declaration's name, for the stale-cache warning
	declared bool   // false: no URL declaration, fetch the URL directly
}

func (s *Source) readRule(ctx context.Context, rawURL, sourceID string, declared *RuleRef, fetch func(context.Context, string) ([]byte, error)) (ruleRead, error) {
	cfg := s.ruleConfig()
	rule, err := rulefetch.ResolveSource(rawURL, cfg, sourceID)
	if err != nil {
		return ruleRead{declared: true}, err
	}
	if rule == nil && declared != nil {
		rule = &model.RuleConfig{Name: declared.Name, Behavior: ptr(declared.Behavior), Format: ptr(declared.Format), SourceType: ptr("url"), URL: ptr(rawURL)}
	}
	if rule == nil || rule.SourceType != nil && *rule.SourceType != "url" {
		return ruleRead{}, nil
	}
	raw, err := model.Marshal(rule)
	if err != nil {
		return ruleRead{}, err
	}
	var library model.RuleLibraryItem
	if err = json.Unmarshal(raw, &library); err != nil {
		return ruleRead{}, err
	}
	value, stale, err := refresh.ReadRemote(ctx, library, refresh.RuleReadOptions{Config: cfg, Fetch: func(ctx context.Context, u string) (rulefetch.Content, error) {
		data, err := fetch(ctx, u)
		return rulefetch.Content{Data: data}, err
	}})
	return ruleRead{data: value, stale: stale, name: rule.Name, declared: true}, err
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
	r := model.RuleConfig{ID: rule.ID, Name: rule.Name, URL: ptr(rule.URL), SourceType: ptr(rule.SourceType), Behavior: ptr(defaultBehavior(rule.Behavior)), Format: ptr(rule.Format)}
	format, err := convert.RuleFormat(r, s.config(), "")
	if err != nil {
		return nil, err
	}
	text, warnings, err := rulefetch.NormalizeRuleset(string(value), defaultBehavior(rule.Behavior), format)
	if len(warnings) > 0 {
		logger.Warningf("Mihomo provider %s: %d unconverted lines", rule.Name, len(warnings))
	}
	return []byte(text), err
}
func (s *Source) validateRemote(value []byte, rule RuleRef) error {
	return rulefetch.ValidateRemote(rulefetch.Content{Data: value}, model.RuleConfig{Name: rule.Name, URL: ptr(rule.URL), SourceType: ptr("url"), Behavior: ptr(rule.Behavior), Format: ptr(rule.Format)}, nil, rule.Format)
}
func (s *Source) unavailable(paths []string) []string {
	profile, err := s.dnsProfile()
	if err != nil {
		return []string{err.Error()}
	}
	return oxidns.UnavailableRulesets(profile, paths)
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
	cfg := s.members
	cfg.Delete("agents")
	return cfg.MarshalJSON()
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

func receiptAgentRaw(agent *model.Agent) receiptAgentView {
	var view receiptAgentView
	raw, err := model.Marshal(agent)
	if err == nil {
		_ = jsondecode.Unmarshal(raw, &view)
	}
	return view
}
func receiptAgent(ctx context.Context, agent *model.Agent) (receiptAgentView, error) {
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
	retained := map[string]bool{digest([]byte(deploymentID)): true, digest(nil): true}
	err := r.ViewAgents(ctx, func(records []model.Agent) error {
		for i := range records {
			agent := &records[i]
			if !agents.AgentMatchesID(agent, agentID) {
				continue
			}
			if agent.AppliedContent != nil && agent.AppliedContent.DeploymentID != "" {
				retained[digest([]byte(agent.AppliedContent.DeploymentID))] = true
			}
			for id, state := range agent.Deployments.All() {
				if agent.Deployments.EntryValid(id) && !finished(state.Status) {
					retained[digest([]byte(id))] = true
				}
			}
			break
		}
		return nil
	})
	return retained, err
}
func finished(status string) bool {
	return status == "succeeded" || status == "failed" || status == "rolled_back"
}
func advanceApplied(ctx context.Context, m *agents.Manager, agent *model.Agent, fingerprint string, files map[string]string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	accepted := false
	if agent == nil || agent.ID == "" || agent.AppliedContent == nil {
		return false, nil
	}
	expected, _, err := agent.MemberJSON("applied_content")
	if err != nil {
		return false, err
	}
	err = m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		if err := tx.Err(); err != nil {
			return err
		}
		for i := range *records {
			current := &(*records)[i]
			if !agents.AgentMatchesID(current, agent.ID) {
				continue
			}
			applied := current.AppliedContent
			raw, _, err := current.MemberJSON("applied_content")
			if err != nil {
				return err
			}
			if applied == nil || !equalJSON(raw, expected) {
				return nil
			}
			latest := current.LatestDeployment
			if latest != nil && !finished(latest.Status) {
				return nil
			}
			fileMap := model.Map[string]{}
			for path, sha := range files {
				fileMap.Set(path, sha)
			}
			applied.Fingerprint, applied.Files = fingerprint, &fileMap
			update := func(state *model.Deployment) {
				if state != nil && state.DeploymentID == applied.DeploymentID && state.PreparedContent != nil {
					state.PreparedContent.Fingerprint, state.PreparedContent.Files = fingerprint, &fileMap
				}
			}
			if state, ok := current.Deployments.Get(applied.DeploymentID); ok {
				update(&state)
				current.Deployments.Set(applied.DeploymentID, state)
			}
			update(latest)
			accepted = true
			break
		}
		return tx.Err()
	})
	return accepted, err
}

func receiptMatches(agent *model.Agent, state *model.Deployment) bool {
	if agent == nil || state == nil {
		return false
	}
	view, err := receiptAgent(context.Background(), agent)
	if err != nil || view.ID == "" || view.ServiceType != "oxidns" {
		return false
	}
	// Activation proof uses original JSON string identities, never R5 display projections.
	var candidate struct {
		ProfileID    string `json:"profile_id"`
		ServiceType  string `json:"service_type"`
		DeploymentID string `json:"deployment_id"`
		SHA256       string `json:"sha256"`
		Prepared     struct {
			Fingerprint string `json:"fingerprint"`
		} `json:"prepared_content"`
	}
	raw, err := model.Marshal(state)
	if err != nil || jsondecode.Unmarshal(raw, &candidate) != nil {
		return false
	}
	if candidate.ServiceType != "oxidns" || candidate.ProfileID == "" || candidate.ProfileID != view.ProfileID || candidate.DeploymentID == "" || candidate.SHA256 == "" || candidate.Prepared.Fingerprint == "" {
		return false
	}
	expected := ValidationReceipt{Version: 1, RulesSHA256: RulesFingerprint(), AgentID: view.ID, DeploymentID: candidate.DeploymentID, ProfileID: candidate.ProfileID, ServiceType: "oxidns", SHA256: candidate.SHA256, Fingerprint: candidate.Prepared.Fingerprint}
	raw, err = encodeJSON(expected)
	if err != nil {
		return false
	}
	proof, _, err := state.MemberJSON("oxidns_validation_receipt")
	return err == nil && equalJSON(raw, proof)
}

func publicDeployment(record *model.Deployment) *model.Deployment {
	if record == nil {
		return nil
	}
	raw, err := model.Marshal(record)
	if err != nil {
		return nil
	}
	secrets := redact.CollectCredentials(json.RawMessage(raw), nil)
	var out model.Deployment
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	if out.HAPublication != nil {
		outPtr, err := agents.HAAggregateModel(&out, out.HAPublication)
		if err != nil {
			return nil
		}
		out = *outPtr
	}
	if out.Clear("prepared_content") != nil || out.Clear("oxidns_validation_receipt") != nil {
		return nil
	}
	localized, err := agents.PublicDeploymentMessagesModel(&out)
	if err != nil {
		return nil
	}
	raw, err = model.Marshal(localized)
	if err != nil {
		return nil
	}
	raw = redact.SanitizeCredentials(json.RawMessage(raw), secrets)
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return &out
}

// AgentIdentity freezes fields that affect target adaptation, including the
// reported Mihomo core. Canonical JSON is a transient native identity.
func AgentIdentity(t Target) []byte {
	record := t.Record
	if record == nil {
		record = &model.Agent{ID: t.ID, ProfileID: t.ProfileID, ServiceType: t.ServiceType, Host: t.Host, Token: t.Token, Version: t.Version, DeploymentMethod: t.DeploymentMethod, DomainDiscoveryEnabled: &t.DomainDiscovery}
		if len(t.Port) > 0 {
			_ = record.SetJSON("port", t.Port)
		}
	}
	identity := map[string]json.RawMessage{}
	for _, key := range []string{"id", "profile_id", "service_type", "host", "port", "token", "enabled", "version", "deployment_method", "domain_discovery_enabled", "oxidns_ha"} {
		raw, _, err := record.MemberJSON(key)
		if err != nil {
			return nil
		}
		if len(raw) == 0 {
			raw = []byte("null")
		}
		identity[key] = raw
	}
	core := reportedCore(context.Background(), Target{Record: record})
	identity["core_version"] = json.RawMessage("null")
	if core != "" {
		identity["core_version"], _ = json.Marshal(core)
	}
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
	config, revision, err := m.Repository().PublishConfig(ctx, profileID, false)
	if err != nil {
		return nil, 0, err
	}
	raw, err := model.Marshal(config)
	if err != nil {
		return nil, 0, err
	}
	source, err := SourceFromJSON(raw, &revision)
	return source, revision, err
}
func previewTarget(ctx context.Context, m *agents.Manager, id string) (Target, bool, error) {
	agent, err := m.ReadAgent(ctx, id, false)
	if err != nil || agent == nil {
		return Target{}, false, err
	}
	target, err := NewTypedTarget(agent)
	return target, err == nil, err
}
func queryCapabilities(ctx context.Context, m *agents.Manager, t Target) (Capabilities, error) {
	timeout := agents.Seconds(5)
	response, err := m.DeploymentRequestModel(ctx, t.Record, "GET", "capabilities", nil, nil, &timeout)
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
	response, err := m.DeploymentRequestModel(ctx, t.Record, "POST", "config/final-preview", http.Header{"Content-Type": []string{"application/json"}}, body, &timeout)
	if err != nil {
		return "", err
	}
	raw, present, failure := response.MemberJSON("content")
	var value string
	ok := present && failure == nil && json.Unmarshal(raw, &value) == nil
	success := response.Success
	successOK := true
	if !successOK || !success || !ok {
		message := ""
		if response.Message != nil {
			message = *response.Message
		}
		if message == "" {
			message = msgFinalPreviewFailed
		}
		return "", preparation(message)
	}
	return value, nil
}

func (s *Source) githubSource(rawURL string) string {
	candidates, err := rulefetch.DownloadCandidates(rawURL, s.ruleConfig(), "")
	if err == nil && len(candidates) > 0 {
		return candidates[0].URL
	}
	return rawURL
}

func generateAgentConfig(ctx context.Context, s *Source, t Target, base string) (AgentConfig, error) {
	var text string
	var err error
	switch t.ServiceType {
	case "", "mihomo":
		text, err = convert.MihomoConfig(ctx, s.config(), convert.MihomoOptions{BaseURL: base, RegionProbe: t.DomainDiscovery, CoreVersion: coreVersionJSON(ctx, t), ForAgent: true})
	case "mosdns":
		text, err = mosdns.Generate(s.config(), base)
	case "oxidns":
		profile, e := s.dnsProfile()
		if e != nil {
			return AgentConfig{}, e
		}
		text, err = oxidns.Generate(profile, base)
	case "surge":
		text, err = convert.SurgeConfig(ctx, s.config(), base)
	default:
		return AgentConfig{}, preparation("Unsupported service type: " + t.ServiceType)
	}
	sum := md5.Sum([]byte(text))
	hash := hex.EncodeToString(sum[:])
	return AgentConfig{Content: text, MD5: hash, Version: hash[:8], ServiceType: t.ServiceType}, err
}
func reportedCore(ctx context.Context, t Target) string {
	if t.Record == nil {
		return ""
	}
	store, err := trafficstore.GetNative(ctx)
	if err != nil {
		return ""
	}
	raw, err := model.Marshal(t.Record)
	if err != nil {
		return ""
	}
	var agent subs.CoreAgent
	if json.Unmarshal(raw, &agent) != nil {
		return ""
	}
	return subs.AgentCoreVersion(agent, func(id string) subs.CoreStatus {
		status, _ := subs.DecodeCoreStatus(store.StatusJSON(id))
		return status
	})
}
func prepareContent(ctx context.Context, m *agents.Manager, t Target, s *Source, o PrepareOptions) (*Prepared, error) {
	if s == nil {
		return nil, preparation("Missing profile snapshot")
	}
	cfg := s.config()
	p := &Prepared{Providers: []*provider.Artifact{}, ProviderDownloads: []ProviderFile{}, CustomFiles: []CustomFile{}, RulesetDownloads: []convert.Download{}}
	service := t.ServiceType
	if service == "" {
		service = "mihomo"
	}
	if service == "mihomo" {
		if s.gateway().Enabled() {
			if err := gateway.RequireGatewayAgent(t.Version); err != nil {
				return nil, err
			}
		}
		marker, _ := routersplit.ReadMarkerProfile(cfg.Profile)
		if marker != "absent" && t.DeploymentMethod != "docker" && !gateway.SupportsForwarding(t.Version) {
			return nil, preparation(fmt.Sprintf(msgForwardingUpgrade, gateway.ForwardingMinAgentVersion))
		}
	}
	if o.Progress.Phase != nil {
		o.Progress.Phase(fmt.Sprintf(msgGenerating, map[string]string{"mihomo": "Mihomo", "mosdns": "MosDNS", "oxidns": "OxiDNS"}[service]))
	}
	var err error
	switch service {
	case "mihomo":
		conversions := &convert.Conversions{}
		report := &convert.GenerationReport{}
		generate := func(reuse bool) (*yamlx.Node, error) {
			var e error
			p.ConfigContent, e = convert.MihomoConfig(convert.WithConversions(ctx, conversions, reuse), cfg, convert.MihomoOptions{BaseURL: o.BaseURL, SyncLANHosts: true, SkipProviderPreflight: true, RegionProbe: t.DomainDiscovery, CoreVersion: coreVersionJSON(ctx, t), Report: report, ForAgent: true})
			if e != nil {
				return nil, e
			}
			node, e := yamlx.Parse([]byte(p.ConfigContent))
			if e != nil {
				return nil, generatedFailure(e, p.ConfigContent, "Mihomo")
			}
			if node == nil || yamlx.Resolve(node).Kind != yaml.MappingNode {
				return nil, preparation("Generated Mihomo configuration must be an object")
			}
			return node, nil
		}

		main, e := generate(false)
		if e != nil {
			return nil, e
		}
		publicationHooks.RLock()
		guards := publicationHooks.guards
		publicationHooks.RUnlock()
		if isFullTun(cfg.Profile) {
			if e = routersplit.RequireDocument(main); e != nil {
				return nil, e
			}
			if t.DeploymentMethod != "docker" {
				if guards.RequireForwardingKept == nil {
					return nil, ErrGatewayGuardMissing
				}
				if e = guards.RequireForwardingKept(ctx, m, t); e != nil {
					return nil, e
				}
			}
		} else if t.DeploymentMethod != "docker" && !isTrue(yamlx.Get(yamlx.Get(main, routersplit.GatewayMarker), "enabled")) && gateway.SupportsGateway(t.Version) {
			source, e := routersplit.PublishSource(ctx, m.Repository(), t.ProfileID, cfg.Profile, t.ID)
			if e != nil {
				return nil, e
			}
			if source != "" {
				used, e := routersplit.WasGatewayUsed(cfg.Profile, t.Record, source)
				if e != nil {
					return nil, e
				}
				if used {
					if guards.RequireGatewayOffKept == nil {
						return nil, ErrGatewayGuardMissing
					}
					if e = guards.RequireGatewayOffKept(ctx, m, t); e != nil {
						return nil, e
					}
				}
			}
		}
		downloads, e := convert.MihomoProviderDownloads(ctx, cfg, o.BaseURL, main)
		if e != nil {
			return nil, e
		}
		if len(downloads) > 0 {
			opts := provider.DeliveryOptions{RenderAll: true, Progress: func(message string, force bool) {
				if !force && o.Progress.Message != nil {
					o.Progress.Message(message)
				} else if o.Progress.Phase != nil {
					o.Progress.Phase(message)
				}
			}}
			if o.PreferCached {
				opts.SubscriptionFetch = func(ctx context.Context, id, url string) (*refresh.SourceResult, error) {
					result, err := substore.Default().Client.Fetch(ctx, id, url, "", substore.FetchOptions{PreferCached: true})
					if err != nil || result == nil {
						return nil, err
					}
					return &refresh.SourceResult{Text: result.Text, Source: result.Source, FetchedAt: result.FetchedAt, Traffic: refreshTraffic(result.Traffic)}, nil
				}
			}
			p.Providers, e = provider.PrepareBundle(convert.WithConversions(ctx, conversions, false), cfg, main, opts)
			if e != nil {
				return nil, e
			}
			updates := map[string]provider.CacheWrite{}
			order := []string{}
			for _, v := range p.Providers {
				for _, u := range v.CacheUpdates {
					if _, ok := updates[u.SubID]; !ok {
						order = append(order, u.SubID)
					}
					updates[u.SubID] = u
				}
			}
			if o.StoreDownloads && len(updates) > 0 {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				list := []provider.CacheWrite{}
				for _, id := range order {
					list = append(list, updates[id])
				}
				if e = provider.CommitWrites(ctx, list); e != nil {
					return nil, e
				}
				if o.Progress.Phase != nil {
					o.Progress.Phase(msgSavedSubscriptions)
				}
				main, e = generate(true)
				if e != nil {
					return nil, e
				}
				opts.SubscriptionFetch = func(context.Context, string, string) (*refresh.SourceResult, error) {
					return nil, errors.New(msgAlreadyFetched)
				}
				opts.Progress = nil
				p.Providers, e = provider.PrepareBundle(convert.WithConversions(ctx, conversions, true), cfg, main, opts)
				if e != nil {
					return nil, e
				}
			}
			for _, v := range p.Providers {
				path := v.LocalPath
				p.ProviderDownloads = append(p.ProviderDownloads, ProviderFile{Name: v.Name, LocalPath: path, URL: v.URL, Content: []byte(v.Content)})
			}
		}
		if s.gateway().Enabled() {
			providers := yamlx.Resolve(yamlx.Get(main, "rule-providers"))
			if providers != nil && providers.Kind == yaml.MappingNode {
				for i := 0; i < len(providers.Content); i += 2 {
					n := providers.Content[i+1]
					if nodeText(yamlx.Get(n, "type")) == "http" && nodeText(yamlx.Get(n, "url")) != "" {
						p.RulesetDownloads = append(p.RulesetDownloads, convert.Download{Name: providers.Content[i].Value, URL: nodeText(yamlx.Get(n, "url")), LocalPath: nodeText(yamlx.Get(n, "path")), Behavior: nodeText(yamlx.Get(n, "behavior")), Format: nodeText(yamlx.Get(n, "format"))})
					}
				}
			}
		} else {
			ds, e := convert.MihomoRulesetDownloads(ctx, cfg, o.BaseURL)
			if e != nil {
				return nil, e
			}
			for _, d := range ds {
				p.RulesetDownloads = append(p.RulesetDownloads, convert.Download{Name: d.Name, URL: d.URL, LocalPath: d.LocalPath, Behavior: d.Behavior, Format: d.Format})
			}
		}
		if report.SubscriptionHosts != nil {
			p.SubscriptionHosts, _ = yamlx.ToJSON(report.SubscriptionHosts)
		} else {
			p.SubscriptionHosts = []byte("null")
		}
		if report.SubscriptionDialers != nil {
			p.SubscriptionDialers, _ = yamlx.ToJSON(report.SubscriptionDialers)
		} else {
			p.SubscriptionDialers = []byte("null")
		}

	case "mosdns":
		p.ConfigContent, err = mosdns.Generate(cfg, o.BaseURL)
		if err != nil {
			return nil, err
		}
		ds, e := mosdns.Downloads(cfg, o.BaseURL)
		if e != nil {
			return nil, e
		}
		for _, v := range ds {
			p.RulesetDownloads = append(p.RulesetDownloads, convert.Download{Name: v.Name, URL: v.URL, LocalPath: v.LocalPath, Behavior: v.Behavior})
		}
		fs, e := mosdns.CustomFiles(cfg)
		if e != nil {
			return nil, e
		}
		for _, v := range fs {
			p.CustomFiles = append(p.CustomFiles, CustomFile{Path: v.Path, Content: []byte(v.Content)})
		}

	case "oxidns":
		profile, e := s.dnsProfile()
		if e != nil {
			return nil, e
		}
		p.ConfigContent, err = oxidns.Generate(profile, o.BaseURL)
		if err != nil {
			return nil, err
		}
		n, e := yamlx.Parse([]byte(p.ConfigContent))
		if e == nil {
			_, e = oxidns.RequireValidDocument(n)
		}
		if e != nil {
			return nil, generatedFailure(e, p.ConfigContent, "OxiDNS")
		}
		ds, e := oxidns.RulesetDownloads(profile, o.BaseURL)
		if e != nil {
			return nil, e
		}
		for _, v := range ds {
			p.RulesetDownloads = append(p.RulesetDownloads, convert.Download{Name: v.Name, URL: v.URL, LocalPath: v.LocalPath, Behavior: scalar(v.Behavior)})
		}
		fs, e := oxidns.CustomFiles(profile)
		if e != nil {
			return nil, e
		}
		for _, v := range fs {
			p.CustomFiles = append(p.CustomFiles, CustomFile{Path: v.Path, Content: v.Content, Native: v.Native})
		}
	default:
		return nil, preparation("Unsupported service type")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	p.Bundle, err = Build(ctx, BuildInput{Target: t, Source: s, ConfigContent: p.ConfigContent, ProviderDownloads: p.ProviderDownloads, RulesetDownloads: p.RulesetDownloads, CustomFiles: p.CustomFiles, BaseURL: o.BaseURL, DeploymentID: o.DeploymentID, LocalDNSWatchdog: true, Progress: o.Progress})
	return p, err
}
func preparationFailure(err error) bool {
	var strategy *rules.StrategyReferenceError
	var base *convert.BaseSettingsError
	var easy *convert.EasyTierSettingsError
	var gatewayErr *gateway.Error
	var split *routersplit.RouterSplitError
	var settings *oxidns.SettingsError
	var validation *oxidns.ValidationError
	var reference *oxidns.ReferenceError
	var remote *rulefetch.RemoteRuleValidationError
	var agent *agents.ValueError
	var archive *mosdns.ArchiveError
	var rs *routersplit.ValidationError
	return errors.Is(err, setupflow.ErrInvalidSession) || errors.As(err, &strategy) || errors.As(err, &base) || errors.As(err, &easy) || errors.As(err, &gatewayErr) || errors.As(err, &split) || errors.As(err, &settings) || errors.As(err, &validation) || errors.As(err, &reference) || errors.As(err, &remote) || errors.As(err, &agent) || errors.As(err, &archive) || repo.IsValueError(err) || errors.As(err, &rs)
}

// Raw returns an owned JSON snapshot, without exposing mutable legacy documents.
func (s *Source) Raw() (json.RawMessage, error) {
	if s == nil {
		return nil, fmt.Errorf("missing captured configuration")
	}
	return s.members.MarshalJSON()
}

// Member returns an owned source member, or null for an absent member.
func (s *Source) Member(key string) json.RawMessage {
	if s == nil {
		return []byte("null")
	}
	raw, ok := s.members.Get(key)
	if !ok {
		return []byte("null")
	}
	return raw
}

// WithMember captures a new snapshot with one raw member replaced.
func (s *Source) WithMember(key string, raw json.RawMessage) (*Source, error) {
	members := s.members
	if err := members.Set(key, raw); err != nil {
		return nil, err
	}
	data, err := members.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return SourceFromJSON(data, s.revision)
}

// SourceFromJSON owns the raw snapshot and its decoded model view.
func SourceFromJSON(raw json.RawMessage, revision *int64) (*Source, error) {
	var members model.Fields
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil, preparation("Profile snapshot must be an object")
	}
	config, err := model.DecodeConfig(raw)
	if err != nil {
		return nil, err
	}
	source := &Source{members: members, modelConfig: config}
	if revision != nil {
		value := *revision
		source.revision = &value
	}
	return source, nil
}

// CanonicalDigest hashes native canonical JSON (D2); it is shared by consumers.
func CanonicalDigest(raw json.RawMessage) string { return SettingsDigest(raw) }

func targetByID(ctx context.Context, m *agents.Manager, id string) (Target, bool, error) {
	a, e := m.ReadAgent(ctx, id, true)
	if e != nil || a == nil {
		return Target{}, false, e
	}
	t, e := NewTypedTarget(a)
	if t.ProfileID == "" {
		t.ProfileID, e = m.Repository().DefaultProfileID(ctx)
	}
	if t.ServiceType == "" {
		t.ServiceType = "mihomo"
	}
	return t, true, e
}

type taskView struct {
	Success        bool   `json:"success"`
	Existing       bool   `json:"existing"`
	Submitted      bool   `json:"submitted"`
	ManualRollback bool   `json:"manual_rollback"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	HTTPStatus     int    `json:"http_status"`
	DeploymentID   string `json:"deployment_id"`
	ProfileID      string `json:"profile_id"`
	ServiceType    string `json:"service_type"`
	SHA256         string `json:"sha256"`
	SourceSHA256   string `json:"source_sha256"`
}

func resultView(r *model.Deployment) taskView {
	var v taskView
	b, _ := model.Marshal(r)
	_ = jsondecode.Unmarshal(b, &v)
	return v
}
func taskViewOf(t Target, id string) (taskView, bool) {
	r := taskResult(t, id)
	return resultView(r), r != nil
}
func taskResult(t Target, id string) *model.Deployment {
	if t.Record == nil || !t.Record.Deployments.EntryValid(id) {
		return nil
	}
	r, ok := t.Record.Deployments.Get(id)
	if !ok {
		return nil
	}
	return &r
}
func result(success bool, status, message string, httpStatus int) *model.Deployment {
	body := map[string]any{"success": success}
	if status != "" {
		body["status"] = status
	}
	if message != "" {
		body["message"] = message
	}
	if httpStatus != 0 {
		body["http_status"] = httpStatus
	}
	raw, _ := encodeJSON(body)
	var out model.Deployment
	_ = json.Unmarshal(raw, &out)
	return &out
}
func resultWithID(r *model.Deployment, id string) *model.Deployment {
	return resultField(r, "deployment_id", id)
}
func resultField(r *model.Deployment, key string, value any) *model.Deployment {
	raw, _ := model.Marshal(r)
	var fields model.Fields
	_ = json.Unmarshal(raw, &fields)
	encoded, _ := model.Marshal(value)
	fields.Set(key, encoded)
	raw, _ = model.Marshal(fields)
	var out model.Deployment
	_ = json.Unmarshal(raw, &out)
	return &out
}
func recordUpdate(ctx context.Context, m *agents.Manager, agentID, id string, u Update, o agents.RecordOptions) (*model.Deployment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	patch := map[string]any{}
	for key, p := range map[string]*string{"status": u.Status, "message": u.Message, "error": u.Error, "error_details": u.ErrorDetails, "failed_stage": u.FailedStage, "sha256": u.SHA256, "source_sha256": u.SourceSHA256, "config_version": u.ConfigVersion} {
		if p != nil {
			patch[key] = *p
		}
	}
	for key, p := range map[string]*bool{"success": u.Success, "submitted": u.Submitted, "manual_rollback": u.ManualRollback} {
		if p != nil {
			patch[key] = *p
		}
	}
	if u.HTTPStatus != 0 {
		patch["http_status"] = u.HTTPStatus
	}
	if u.ConfigRevision != nil {
		if u.ConfigRevision.Null {
			patch["config_revision"] = nil
		} else {
			patch["config_revision"] = u.ConfigRevision.Value
		}
	}
	raw, err := encodeJSON(patch)
	if err != nil {
		return nil, err
	}
	update, err := agents.DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	return m.RecordDeploymentModel(ctx, agentID, id, update, o)
}
func normalizeUpload(r *model.Deployment) *model.Deployment {
	if r.HTTPStatus != nil && *r.HTTPStatus != 0 {
		if r.Status == "" {
			return resultField(r, "status", "failed")
		}
	} else {
		raw, present, err := r.MemberJSON("status")
		var text string
		if err != nil || !present || json.Unmarshal(raw, &text) != nil {
			return result(true, "unknown", msgUploadAccepted, 0)
		}
	}
	return r
}
func pairRequired(t Target) bool {
	return agents.HAEnabledModel(t.Record) || agents.HAActiveModel(t.Record)
}
func legacyPush(ctx context.Context, m *agents.Manager, id, content string, o PushInput) (*model.Deployment, error) {
	extra := model.Fields{}
	if o.RestartAfterUpdate {
		_ = extra.Set("restart_after_update", []byte("true"))
	}
	revision := json.RawMessage("null")
	if o.Publish.ConfigRevision != nil && !o.Publish.ConfigRevision.Null {
		revision, _ = json.Marshal(o.Publish.ConfigRevision.Value)
	}
	raw, err := m.PushLegacyConfigModel(ctx, id, content, agents.LegacyConfigOptions{Extra: extra, Revision: revision, ExpectedProfileID: o.ExpectedProfileID, ExpectedServiceType: o.ExpectedServiceType})
	if err != nil {
		return nil, err
	}
	var out model.Deployment
	err = json.Unmarshal(raw, &out)
	return &out, err
}
func uploadRecord(ctx context.Context, m *agents.Manager, agentID, id string, r *model.Deployment) (*model.Deployment, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := model.Marshal(r)
	if err != nil {
		return nil, err
	}
	update, err := agents.DecodeDeploymentUpdate(raw)
	if err != nil {
		return nil, err
	}
	return m.RecordDeploymentModel(ctx, agentID, id, update, agents.RecordOptions{UploadReceipt: true})
}

// InternalRuleURL reports whether a URL is a callback into this source snapshot.
// Rule synchronization uses the same classification as materialization.
func (s *Source) InternalRuleURL(rawURL, baseURL string) bool { return s.internal(rawURL, baseURL) }

func (s *Source) dnsProfile() (*oxidns.Profile, error) { return oxidns.NewTypedProfile(s.config()) }

// NewTypedTarget captures a typed Agent for the remaining deploy consumers.
// Only this compatibility edge transcodes it; Agent owners keep the model.
func NewTypedTarget(record *model.Agent) (Target, error) {
	if record == nil {
		return Target{}, nil
	}
	data, err := model.Marshal(record)
	if err != nil {
		return Target{}, err
	}
	var owned model.Agent
	if err = json.Unmarshal(data, &owned); err != nil {
		return Target{}, err
	}
	port, _, err := owned.MemberJSON("port")
	if err != nil {
		return Target{}, err
	}
	target := Target{Port: port, Record: &owned}
	// Captured identities/capabilities retain the original string/bool token types.
	for _, field := range []struct {
		key         string
		destination *string
	}{
		{"id", &target.ID}, {"name", &target.Name}, {"profile_id", &target.ProfileID}, {"service_type", &target.ServiceType}, {"deployment_method", &target.DeploymentMethod}, {"version", &target.Version}, {"host", &target.Host}, {"token", &target.Token},
	} {
		raw, _, err := owned.MemberJSON(field.key)
		if err != nil {
			return Target{}, err
		}
		_ = json.Unmarshal(raw, field.destination)
	}
	raw, _, err := owned.MemberJSON("domain_discovery_enabled")
	if err != nil {
		return Target{}, err
	}
	_ = json.Unmarshal(raw, &target.DomainDiscovery)
	return target, nil
}

// ViewConfig reads the cached typed view of an immutable captured snapshot.
// fn must neither modify nor retain the model. The snapshot is decoded once,
// rather than once per generator/Agent operation on the same Source.
func (s *Source) ViewConfig(fn func(*model.Config) error) error {
	if s == nil || s.modelConfig == nil {
		return fmt.Errorf("missing captured configuration")
	}
	return fn(s.modelConfig)
}
func coreVersionJSON(ctx context.Context, t Target) json.RawMessage {
	raw, _ := json.Marshal(reportedCore(ctx, t))
	return raw
}
func isFullTun(profile *model.Profile) bool {
	state, _ := routersplit.ReadMarkerProfile(profile)
	return state == routersplit.StateValid
}

func textValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func nodeText(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n == nil {
		return ""
	}
	return n.Value
}
func refreshTraffic(t *substore.Traffic) *refresh.Traffic {
	if t == nil {
		return nil
	}
	return &refresh.Traffic{Upload: t.Upload, Download: t.Download, Total: t.Total, Expire: t.Expire, Fields: t.Fields, Raw: t.Raw}
}
