package discovery

// This file is the boundary to the pre-N8 repository, matcher and health APIs.
// Business logic uses native structs; these conversions disappear with N8.
import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
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

// legacyLibraryObject projects only the consumed typed members. Storage's R2
// provenance stays on libraryItem; old collaborators must not reinterpret an
// original numeric token after the shared model has decoded it as text.
func legacyLibraryObject(item libraryItem) (*jsonx.Object, error) {
	value, err := object(item)
	if err != nil {
		return nil, err
	}
	value.Set("id", item.ID)
	value.Set("name", item.Name)
	value.Set("source_type", item.SourceType)
	value.Set("behavior", item.Behavior)
	value.Set("content", item.Content)
	return value, nil
}

func legacySaveRule(ctx context.Context, item libraryItem) error {
	v, err := legacyLibraryObject(item)
	if err != nil {
		return err
	}
	_, err = rulefetch.SaveRuleToLocal(lifecycle.Detach(ctx), v, nil)
	return err
}
func legacyValidateContent(item libraryItem) error {
	v, err := legacyLibraryObject(item)
	if err != nil {
		return err
	}
	return rulefetch.ValidateDomainContent(v, nil)
}
func legacyCachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	v, err := legacyLibraryObject(item)
	if err != nil {
		return nil, err
	}
	b, err := rulefetch.ReadValidatedRuleCache(ctx, v, nil)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func legacyMatcherOptions() rules.MatcherCacheOptions {
	return rules.MatcherCacheOptions{
		Clock: lifecycle.ClockFrom(context.Background()), Spawner: matcherSpawner{},
		RulesDir: func() string { return filepath.Join(repo.DataDir(), "shared", "rules") },
		StoredLibrary: func() []any {
			r, err := repo.GetRepository(context.Background())
			if err != nil {
				return nil
			}
			o, err := r.Shared(context.Background())
			if err != nil {
				return nil
			}
			values := make([]any, 0, len(o.RuleLibrary))
			for _, item := range o.RuleLibrary {
				value, err := legacy(item)
				if err != nil {
					return nil
				}
				values = append(values, value)
			}
			return values
		},
		ValidateRemote: func(content string, rule *jsonx.Object) error {
			return rulefetch.ValidateRemoteRuleContent(content, rule, nil, "")
		},
		MaxMatchers: 4, MaxHostResults: 1024, IdleSeconds: 300,
	}
}

func legacyLocalMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return nil, err
	}
	view, err := r.Config(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	c, err := object(view)
	if err != nil {
		return nil, err
	}
	m := cache(ctx).LocalMatcher(c, id)
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

func legacySupported(version, minimum string) bool {
	return version != "" && agents.CompareVersions(version, minimum) >= 0
}
func legacyLoadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
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
func legacyProbes(ctx context.Context, as []Agent) (map[string]Probe, error) {
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
func legacySaveProbes(ctx context.Context, id string, ps map[string]Probe) error {
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
func legacyJudge(d, p *PathStats, failing, dh, ph bool) (Probe, error) {
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
func legacyRegionData(ctx context.Context, id string) (regionRecord, error) {
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
func legacyEditRegion(ctx context.Context, id string, edit func(*regionRecord) error) error {
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
func legacyKnownService(host string) json.RawMessage {
	v := rulefetch.KnownService(host)
	if v == nil {
		return nil
	}
	b, _ := jsonx.Compact(v)
	return json.RawMessage(b)
}
func legacyPageIdentity(result RegionResult, host, at string) (WebsitePage, error) {
	v, err := object(result)
	if err != nil {
		return WebsitePage{}, err
	}
	var out WebsitePage
	err = decode(rulefetch.PageIdentity(v, host, at), &out)
	return out, err
}
func legacyPublicServices() json.RawMessage {
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
func legacyProbeControls() ([]ProbeTarget, error) {
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
func legacyPickTargets(ts []AvailableTarget, preferred *string) ([]RegionTarget, error) {
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
func legacyRegionRequests(services bool, plan []ProbeTarget) ([]PageRequest, error) {
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
func legacyExitInfo(rows []RegionResult) (json.RawMessage, error) {
	v, err := legacy(rows)
	if err != nil {
		return nil, err
	}
	result, err := check.ExitInfo(v.([]any))
	if err != nil {
		return nil, err
	}
	b, err := jsonx.Compact(result)
	return json.RawMessage(b), err
}
func legacyServiceMatrix(rows []RegionResult) (json.RawMessage, error) {
	v, err := legacy(rows)
	if err != nil {
		return nil, err
	}
	result, err := check.EvaluateServices(v.([]any))
	if err != nil {
		return nil, err
	}
	b, err := jsonx.Compact(result)
	return json.RawMessage(b), err
}
func legacyClassifyRegion(result *RegionResult, url string) (json.RawMessage, error) {
	var value any
	var err error
	if result != nil {
		value, err = legacy(result)
		if err != nil {
			return nil, err
		}
	}
	classified, err := check.ClassifyDomainResult(value, url)
	if err != nil {
		return nil, err
	}
	b, err := jsonx.Compact(classified)
	return json.RawMessage(b), err
}
func legacySummarizeRegion(names []string, per map[string]json.RawMessage) (domainVerdict, error) {
	legacyValues := jsonx.NewObject()
	for _, name := range names {
		if raw, ok := per[name]; ok {
			value, err := jsonx.Parse(raw)
			if err != nil {
				return domainVerdict{}, err
			}
			legacyValues.Set(name, value)
		}
	}
	result, err := check.SummarizeDomain(legacyValues)
	var out domainVerdict
	if err == nil {
		err = decode(result, &out)
	}
	return out, err
}

func legacyServiceDomains(id string) ([]string, bool) {
	s, ok := check.ServiceByID(id)
	return s.Domains, ok
}
func legacyWebsiteMarkers() map[string]string {
	out := map[string]string{}
	for _, m := range rulefetch.PageMarkers {
		out[m.Key] = m.Pattern
	}
	return out
}

func init() {
	collaboration = collaborators{
		saveRule:        legacySaveRule,
		validateContent: legacyValidateContent,
		cachedContent:   legacyCachedContent,
		matcherOptions:  legacyMatcherOptions,
		localMatcher:    legacyLocalMatcher,
		supported:       legacySupported,
		loadTraffic:     legacyLoadTraffic,
		probes:          legacyProbes,
		saveProbes:      legacySaveProbes,
		judge:           legacyJudge,
		regionData:      legacyRegionData,
		editRegion:      legacyEditRegion,
		knownService:    legacyKnownService,
		pageIdentity:    legacyPageIdentity,
		publicServices:  legacyPublicServices,
		probeControls:   legacyProbeControls,
		pickTargets:     legacyPickTargets,
		regionRequests:  legacyRegionRequests,
		exitInfo:        legacyExitInfo,
		serviceMatrix:   legacyServiceMatrix,
		classifyRegion:  legacyClassifyRegion,
		summarizeRegion: legacySummarizeRegion,
		serviceDomains:  legacyServiceDomains,
		websiteMarkers:  legacyWebsiteMarkers,
	}
	client = managerClient{}
}
