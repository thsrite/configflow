package discovery

// This file is the boundary to the pre-N8 repository, matcher and health APIs.
// Business logic uses native structs; these conversions disappear with N8.
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/convert"
	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"gopkg.in/yaml.v3"
)

func decode(v any, out any) error {
	b, err := jsonx.Compact(v)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(b), out)
}
func legacy(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(b)
}
func object(v any) (*jsonx.Object, error) {
	x, err := legacy(v)
	if err != nil {
		return nil, err
	}
	o, ok := x.(*jsonx.Object)
	if !ok {
		return nil, fmt.Errorf("expected JSON object")
	}
	return o, nil
}
func loadConfig(ctx context.Context, id string) (config, error) {
	v, err := repo.GetConfig(ctx, id)
	c := config{Settings: defaultSettings()}
	if err == nil {
		err = decode(v, &c)
	}
	c.Settings.normalize()
	return c, err
}
func editProfile(ctx context.Context, id string, edit func(*config) error) error {
	_, err := repo.UpdateConfigTransaction(context.WithoutCancel(ctx), func(ctx context.Context, o *jsonx.Object) (*jsonx.Object, error) {
		c := config{Settings: defaultSettings()}
		if err := decode(o, &c); err != nil {
			return nil, err
		}
		c.Settings.normalize()
		before, err := json.Marshal(c.Rules)
		if err != nil {
			return nil, err
		}
		if err := edit(&c); err != nil {
			return nil, err
		}
		s, err := legacy(c.Settings)
		if err != nil {
			return nil, err
		}
		r, err := legacy(c.Rules)
		if err != nil {
			return nil, err
		}
		o.Set("domain_discovery", s)
		after, err := json.Marshal(c.Rules)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(before, after) {
			o.Set("rule_configs", r)
		}
		return nil, nil
	}, id)
	return err
}
func editSettings(ctx context.Context, id string, edit func(*Settings) error) error {
	return editProfile(ctx, id, func(c *config) error { return edit(&c.Settings) })
}
func editLibrary(ctx context.Context, edit func(*[]libraryItem) error) error {
	_, err := repo.UpdateSharedConfigTransaction(context.WithoutCancel(ctx), func(ctx context.Context, o *jsonx.Object) (*jsonx.Object, error) {
		var items []libraryItem
		if err := decode(o.GetOr("rule_library", []any{}), &items); err != nil {
			return nil, err
		}
		if err := edit(&items); err != nil {
			return nil, err
		}
		v, err := legacy(items)
		if err != nil {
			return nil, err
		}
		o.Set("rule_library", v)
		return nil, nil
	})
	return err
}
func saveRule(ctx context.Context, item libraryItem) error {
	v, err := object(item)
	if err != nil {
		return err
	}
	_, err = rulefetch.SaveRuleToLocal(context.WithoutCancel(ctx), v, nil)
	return err
}
func validateContent(item libraryItem) error {
	v, err := object(item)
	if err != nil {
		return err
	}
	return rulefetch.ValidateDomainContent(v, nil)
}
func cachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	v, err := object(item)
	if err != nil {
		return nil, err
	}
	b, err := rulefetch.ReadValidatedRuleCache(ctx, v, nil)
	if err != nil {
		return nil, err
	}
	if b == nil {
		b = []byte(item.Content)
	}
	return b, nil
}

var matcherOnce sync.Once
var matcherCache *rules.MatcherCache

func cache() *rules.MatcherCache {
	matcherOnce.Do(func() {
		matcherCache = rules.NewMatcherCache(matcherOptions())
		lifecycle.OnShutdown("domain-discovery-cache", func(context.Context) { matcherCache.Clear() })
	})
	return matcherCache
}

func matcherOptions() rules.MatcherCacheOptions {
	return rules.MatcherCacheOptions{
		RulesDir: func() string { return filepath.Join(repo.DataDir(), "shared", "rules") },
		StoredLibrary: func() []any {
			r, err := repo.GetRepository(context.Background())
			if err != nil {
				return nil
			}
			o, err := r.GetShared(context.Background())
			if err != nil {
				return nil
			}
			return o.GetList("rule_library")
		},
		ValidateRemote: func(content string, rule *jsonx.Object) error {
			return rulefetch.ValidateRemoteRuleContent(content, rule, nil, "")
		},
		MaxMatchers: 4, MaxHostResults: 1024, IdleSeconds: 300,
	}
}

// Match is the public rule-match metadata attached to discovered hosts.
type Match struct {
	RuleID   string                     `json:"rule_id"`
	Priority int                        `json:"priority"`
	RuleName string                     `json:"rule_name"`
	Policy   string                     `json:"policy"`
	Extra    map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *Match) UnmarshalJSON(b []byte) error {
	type plain Match
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = Match(p)
	return capture(b, &v.Extra, "rule_id", "priority", "rule_name", "policy")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v Match) MarshalJSON() ([]byte, error) { type plain Match; return preserve(plain(v), v.Extra) }
func localMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	c, err := repo.GetConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	m := cache().LocalMatcher(c, id)
	return func(host string) *Match {
		v := m.Match(host)
		if v == nil {
			return nil
		}
		var out Match
		if decode(v, &out) != nil {
			return nil
		}
		return &out
	}, nil
}

// Agent is the small registry view retained while a discovery operation runs.
type Agent struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProfileID   string `json:"profile_id"`
	ServiceType string `json:"service_type"`
	Enabled     bool   `json:"domain_discovery_enabled"`
	Version     string `json:"version"`
	Heartbeat   string `json:"last_heartbeat"`
}

func profileAgents(ctx context.Context, id string) ([]Agent, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	all, err := m.GetAllAgents(ctx, false)
	if err != nil {
		return nil, err
	}
	def, err := repo.GetDefaultProfileID(ctx)
	if err != nil {
		return nil, err
	}
	out := []Agent{}
	for _, v := range all {
		var a Agent
		view := jsonx.NewObject()
		for _, key := range []string{"id", "name", "profile_id", "service_type", "domain_discovery_enabled", "version", "last_heartbeat"} {
			if field, ok := v.Get(key); ok {
				view.Set(key, field)
			}
		}
		if err := decode(view, &a); err != nil {
			return nil, err
		}
		if a.ProfileID == "" {
			a.ProfileID = def
		}
		if a.ProfileID == id && a.ServiceType == "mihomo" {
			out = append(out, a)
		}
	}
	return out, nil
}
func supported(version, minimum string) bool {
	return version != "" && agents.CompareVersions(version, minimum) >= 0
}
func loadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
	s, err := trafficstore.Get(ctx)
	if err != nil {
		return traffic{}, err
	}
	var out traffic
	if full {
		err = decode(s.Load(a.ID, nil), &out)
	} else {
		err = decode(s.LoadStatus(a.ID), &out.Status)
	}
	return out, err
}
func probes(ctx context.Context, as []Agent) (map[string]Probe, error) {
	store, err := health.GetProbeStore(ctx)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, a := range as {
		ids = append(ids, a.ID)
	}
	v, err := check.LatestProbes(ids, store)
	if err != nil {
		return nil, err
	}
	out := map[string]Probe{}
	err = decode(v, &out)
	return out, err
}
func saveProbes(ctx context.Context, id string, ps map[string]Probe) error {
	store, err := health.GetProbeStore(ctx)
	if err != nil {
		return err
	}
	v, err := object(ps)
	if err != nil {
		return err
	}
	return store.Save(id, v, now())
}
func judge(d, p *PathStats, failing, dh, ph bool) (Probe, error) {
	dv, err := legacy(d)
	if err != nil {
		return Probe{}, err
	}
	pv, err := legacy(p)
	if err != nil {
		return Probe{}, err
	}
	v, err := check.Judge(dv, pv, check.JudgeOptions{PassiveFailing: failing, DirectHealthy: dh, ProxyHealthy: ph})
	var out Probe
	if err == nil {
		err = decode(v, &out)
	}
	return out, err
}
func regionData(ctx context.Context, id string) (regionRecord, error) {
	s, err := health.GetRegionStore(ctx)
	var out regionRecord
	if err == nil {
		err = decode(s.Load(id), &out)
	}
	if out.Domains == nil {
		out.Domains = map[string]json.RawMessage{}
	}
	if out.Websites == nil {
		out.Websites = map[string]WebsitePage{}
	}
	return out, err
}
func editRegion(ctx context.Context, id string, edit func(*regionRecord) error) error {
	s, err := health.GetRegionStore(ctx)
	if err != nil {
		return err
	}
	return s.Update(id, func(o *jsonx.Object) error {
		var out regionRecord
		if err := decode(o, &out); err != nil {
			return err
		}
		if out.Domains == nil {
			out.Domains = map[string]json.RawMessage{}
		}
		if out.Websites == nil {
			out.Websites = map[string]WebsitePage{}
		}
		if err := edit(&out); err != nil {
			return err
		}
		v, err := object(out)
		if err != nil {
			return err
		}
		o.Update(v)
		return nil
	}, now())
}
func knownService(host string) json.RawMessage {
	v := rulefetch.KnownService(host)
	if v == nil {
		return nil
	}
	b, _ := jsonx.Compact(v)
	return json.RawMessage(b)
}
func pageIdentity(result RegionResult, host, at string) (WebsitePage, error) {
	v, err := object(result)
	if err != nil {
		return WebsitePage{}, err
	}
	var out WebsitePage
	err = decode(rulefetch.PageIdentity(v, host, at), &out)
	return out, err
}
func publicServices() json.RawMessage {
	b, _ := jsonx.Compact(check.PublicServices())
	return json.RawMessage(b)
}

// ProviderRevisions keeps the A05 Agent API compatible until its N8 conversion.
func ProviderRevisions(ctx context.Context, id string) (*jsonx.Object, error) {
	v, err := providerRevisions(ctx, id)
	if err != nil {
		return nil, err
	}
	return object(v)
}

type managerClient struct{}

// Probe sends a bounded domain-probe request through the Agent manager.
func (managerClient) Probe(ctx context.Context, a Agent, p ProbeRequest) ([]ProbeRow, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.GetAgentByID(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	v, err := legacy(p)
	if err != nil {
		return nil, err
	}
	res, err := m.ProbeDomains(ctx, agent, v, 200)
	if err != nil {
		return nil, err
	}
	var rows []ProbeRow
	err = decode(res.Value("results"), &rows)
	return rows, err
}

// Targets reads the available region-check paths from the Agent.
func (managerClient) Targets(ctx context.Context, a Agent) ([]AvailableTarget, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.GetAgentByID(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	res, err := m.RegionTargets(ctx, agent)
	if err != nil {
		return nil, err
	}
	var out []AvailableTarget
	err = decode(res, &out)
	return out, err
}

// Region sends a bounded page-check batch through the Agent manager.
func (managerClient) Region(ctx context.Context, a Agent, p RegionRequest) ([]RegionResult, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.GetAgentByID(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	v, err := legacy(p)
	if err != nil {
		return nil, err
	}
	res, err := m.RegionCheck(ctx, agent, v)
	if err != nil {
		return nil, err
	}
	var out []RegionResult
	err = decode(res.Value("results"), &out)
	return out, err
}
func probeControls() ([]ProbeTarget, error) {
	v := check.ControlTargets()
	out := []ProbeTarget{}
	for _, key := range []string{"direct", "proxy"} {
		var t ProbeTarget
		if err := decode(v.Value(key), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
func pickTargets(ts []AvailableTarget, preferred *string) ([]RegionTarget, error) {
	v, err := legacy(ts)
	if err != nil {
		return nil, err
	}
	var p any
	if preferred != nil {
		p = *preferred
	}
	picked, err := check.PickTargets(v.([]any), p)
	if err != nil {
		return nil, err
	}
	var out []RegionTarget
	err = decode(picked, &out)
	return out, err
}
func regionRequests(services bool, plan []ProbeTarget) ([]PageRequest, error) {
	requests := []any{check.ExitRequest()}
	if services {
		requests = check.ServiceRequests()
	}
	for i, h := range plan {
		requests = append(requests, check.DomainRequest(fmt.Sprintf("domain-%d", i), h.URL))
	}
	var out []PageRequest
	err := decode(requests, &out)
	return out, err
}
func evaluateRegion(targets []RegionTarget, raw map[string][]RegionResult, plan []ProbeTarget, services bool, errors []JobError, at string) (regionRecord, error) {
	out := regionRecord{Domains: map[string]json.RawMessage{}}
	infos := []map[string]any{}
	matrix := map[string]json.RawMessage{}
	for _, t := range targets {
		rows := raw[t.Name]
		var exit any
		var failure *string
		if len(rows) > 0 {
			v, err := legacy(rows)
			if err != nil {
				return out, err
			}
			ex, err := check.ExitInfo(v.([]any))
			if err != nil {
				return out, err
			}
			b, _ := jsonx.Compact(ex)
			exit = json.RawMessage(b)
			if services {
				m, err := check.EvaluateServices(v.([]any))
				if err != nil {
					return out, err
				}
				b, _ := jsonx.Compact(m)
				matrix[t.Name] = json.RawMessage(b)
			}
		}
		for _, e := range errors {
			if e.Target == t.Name {
				failure = ptr(e.Message)
				break
			}
		}
		infos = append(infos, map[string]any{"name": t.Name, "kind": t.Kind, "region_hint": t.RegionHint, "exit": exit, "error": failure})
	}
	if services {
		b, err := json.Marshal(map[string]any{"checked_at": at, "targets": infos, "matrix": matrix})
		if err != nil {
			return out, err
		}
		out.Services = b
	}
	for i, h := range plan {
		per := jsonx.NewObject()
		for _, name := range sortedKeys(raw) {
			rows := raw[name]
			if len(rows) == 0 {
				continue
			}
			var result any
			for _, r := range rows {
				if r.ID == fmt.Sprintf("domain-%d", i) {
					var err error
					result, err = legacy(r)
					if err != nil {
						return out, err
					}
					break
				}
			}
			v, err := check.ClassifyDomainResult(result, h.URL)
			if err != nil {
				return out, err
			}
			per.Set(name, v)
		}
		s, err := check.SummarizeDomain(per)
		if err != nil {
			return out, err
		}
		s.Set("checked_at", at)
		s.Set("url", h.URL)
		s.Set("targets", per)
		iv, err := legacy(infos)
		if err != nil {
			return out, err
		}
		s.Set("target_infos", iv)
		b, err := jsonx.Compact(s)
		if err != nil {
			return out, err
		}
		out.Domains[h.Host] = json.RawMessage(b)
	}
	return out, nil
}
func serviceDomains(id string) ([]string, bool) { s, ok := check.ServiceByID(id); return s.Domains, ok }
func websiteMarkers() map[string]string {
	out := map[string]string{}
	for _, m := range rulefetch.PageMarkers {
		out[m.Key] = m.Pattern
	}
	return out
}

type profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func listProfiles(ctx context.Context) ([]profile, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	v, err := r.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	var out []profile
	err = decode(v, &out)
	return out, err
}

func customPorts(text string) (map[int]bool, error) {
	out := map[int]bool{}
	var parsed map[string]any
	if yaml.Unmarshal([]byte(text), &parsed) != nil || parsed == nil {
		return out, nil
	}
	v, err := object(parsed)
	if err != nil {
		return out, err
	}
	ports, err := convert.MihomoUsedPorts(v)
	if err != nil {
		return out, err
	}
	for n := range ports {
		if v, ok := n.Int64(); ok {
			out[int(v)] = true
		}
	}
	return out, nil
}
