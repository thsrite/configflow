package discovery

// Native collaboration between discovery, registry and health owners.
import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/health"
	"configflow/internal/health/check"
	"configflow/internal/rulefetch"
)

func project[A, B any](v A, out *B) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal([]byte(b), out)
}
func legacySupported(version, minimum string) bool {
	return version != "" && agents.CompareVersions(version, minimum) >= 0
}
func legacyLoadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
	s, err := trafficstore.GetNative(ctx)
	if err != nil {
		return traffic{}, err
	}
	var out traffic
	if full {
		err = project(s.LoadDocument(a.ID, nil), &out)
	} else {
		raw := s.StatusJSON(a.ID)
		if len(raw) > 0 {
			err = jsondecode.Unmarshal(raw, &out.Status)
		}
	}
	return out, err
}
func legacyProbes(ctx context.Context, as []Agent) (map[string]Probe, error) {
	store, err := health.GetProbeStore(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(as))
	for i, a := range as {
		ids[i] = a.ID
	}
	entries, err := check.LatestProbeEntries(ids, store)
	if err != nil {
		return nil, err
	}
	out := map[string]Probe{}
	err = project(entries, &out)
	return out, err
}
func legacySaveProbes(ctx context.Context, id string, ps map[string]Probe) error {
	store, err := health.GetProbeStore(ctx)
	if err != nil {
		return err
	}
	var entries model.Map[check.TimedRecord]
	if err := project(ps, &entries); err != nil {
		return err
	}
	return store.SaveEntriesContext(ctx, id, entries, now())
}
func legacyJudge(d, p *PathStats, failing, dh, ph bool) (Probe, error) {
	var direct, proxy *check.PathStats
	if err := project(d, &direct); err != nil {
		return Probe{}, err
	}
	if err := project(p, &proxy); err != nil {
		return Probe{}, err
	}
	var out Probe
	err := project(check.JudgePath(direct, proxy, check.JudgeOptions{PassiveFailing: failing, DirectHealthy: dh, ProxyHealthy: ph}), &out)
	return out, err
}
func legacyRegionData(ctx context.Context, id string) (regionRecord, error) {
	s, err := health.GetRegionStore(ctx)
	var out regionRecord
	if err == nil {
		err = project(s.LoadData(id), &out)
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
	return s.UpdateDataContext(ctx, id, func(data *check.RegionData) error {
		var out regionRecord
		if err := project(data, &out); err != nil {
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
		// Preserve fields owned by future versions of the shared region store.
		var next check.RegionData
		if err := project(out, &next); err != nil {
			return err
		}
		data.Services, data.Domains, data.Websites = next.Services, next.Domains, next.Websites
		return ctx.Err()
	}, now())
}
func legacyPublicServices() json.RawMessage {
	b, _ := json.Marshal(check.PublicServiceList())
	return json.RawMessage(b)
}

type managerClient struct{}

// Probe sends a bounded domain-probe request through the Agent manager.
func (managerClient) Probe(ctx context.Context, a Agent, p ProbeRequest) ([]ProbeRow, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.ReadAgent(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	res, err := m.ProbeDomainsModel(ctx, agent, payload, agents.Seconds(200))
	if err != nil {
		return nil, err
	}
	var rows []ProbeRow
	raw, _ := res.Get("results")
	err = jsondecode.Unmarshal(raw, &rows)
	return rows, err
}

// Targets reads the available region-check paths from the Agent.
func (managerClient) Targets(ctx context.Context, a Agent) ([]AvailableTarget, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.ReadAgent(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	raw, err := m.RegionTargetsModel(ctx, agent)
	if err != nil {
		return nil, err
	}
	var out []AvailableTarget
	err = jsondecode.Unmarshal(raw, &out)
	return out, err
}

// Region sends a bounded page-check batch through the Agent manager.
func (managerClient) Region(ctx context.Context, a Agent, p RegionRequest) ([]RegionResult, error) {
	m, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := m.ReadAgent(ctx, a.ID, false)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	res, err := m.RegionCheckModel(ctx, agent, payload)
	if err != nil {
		return nil, err
	}
	var out []RegionResult
	raw, _ := res.Get("results")
	err = jsondecode.Unmarshal(raw, &out)
	return out, err
}
func legacyProbeControls() ([]ProbeTarget, error) {
	targets := check.DefaultControlTargets()
	var out []ProbeTarget
	err := project([]check.ProbeTarget{targets.Direct, targets.Proxy}, &out)
	return out, err
}
func legacyPickTargets(ts []AvailableTarget, preferred *string) ([]RegionTarget, error) {
	var targets []check.AvailableTarget
	if err := project(ts, &targets); err != nil {
		return nil, err
	}
	group := ""
	if preferred != nil {
		group = *preferred
	}
	var out []RegionTarget
	err := project(check.ChooseTargets(targets, group), &out)
	return out, err
}
func legacyExitRequest() (PageRequest, error) {
	var out PageRequest
	err := project(check.DefaultExitRequest(), &out)
	return out, err
}
func legacyServiceRequests() ([]PageRequest, error) {
	var out []PageRequest
	err := project(check.BuildServiceRequests(), &out)
	return out, err
}
func legacyDomainRequest(id, url string) (PageRequest, error) {
	var out PageRequest
	err := project(check.BuildDomainRequest(id, url), &out)
	return out, err
}
func legacyRegistryAgents(ctx context.Context) ([]model.Agent, error) {
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	return manager.ReadAgents(ctx, false, nil)
}
func legacyExitInfo(rows []RegionResult) (json.RawMessage, error) {
	var entries []check.RegionResult
	if err := project(rows, &entries); err != nil {
		return nil, err
	}
	result, err := check.SummarizeExit(entries)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func legacyServiceMatrix(rows []RegionResult) (json.RawMessage, error) {
	var entries []check.RegionResult
	if err := project(rows, &entries); err != nil {
		return nil, err
	}
	result, err := check.EvaluateServiceList(entries)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
func legacyClassifyRegion(result *RegionResult, url string) (json.RawMessage, error) {
	var entry *check.RegionResult
	if err := project(result, &entry); err != nil {
		return nil, err
	}
	classified, err := check.ClassifyDomain(entry, url)
	if err != nil {
		return nil, err
	}
	return json.Marshal(classified)
}
func legacySummarizeRegion(names []string, per map[string]json.RawMessage) (domainVerdict, error) {
	var entries model.Map[check.TargetState]
	for _, name := range names {
		if raw, ok := per[name]; ok {
			var entry check.TargetState
			if err := jsondecode.Unmarshal(raw, &entry); err != nil {
				return domainVerdict{}, err
			}
			entries.Set(name, entry)
		}
	}
	var out domainVerdict
	err := project(check.SummarizeTargets(entries), &out)
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
		saveRule:        nativeSaveRule,
		validateContent: nativeValidateContent,
		cachedContent:   nativeCachedContent,
		matcherOptions:  nativeMatcherOptions,
		localMatcher:    nativeLocalMatcher,
		supported:       legacySupported,
		loadTraffic:     legacyLoadTraffic,
		probes:          legacyProbes,
		saveProbes:      legacySaveProbes,
		judge:           legacyJudge,
		regionData:      legacyRegionData,
		editRegion:      legacyEditRegion,
		knownService:    nativeKnownService,
		pageIdentity:    nativePageIdentity,
		publicServices:  legacyPublicServices,
		probeControls:   legacyProbeControls,
		pickTargets:     legacyPickTargets,
		exitRequest:     legacyExitRequest,
		serviceRequests: legacyServiceRequests,
		domainRequest:   legacyDomainRequest,
		registryAgents:  legacyRegistryAgents,
		exitInfo:        legacyExitInfo,
		serviceMatrix:   legacyServiceMatrix,
		classifyRegion:  legacyClassifyRegion,
		summarizeRegion: legacySummarizeRegion,
		serviceDomains:  legacyServiceDomains,
		websiteMarkers:  legacyWebsiteMarkers,
	}
	client = managerClient{}
}

// ProviderRevisions returns provider hashes for the existing Agent traffic protocol.
func ProviderRevisions(ctx context.Context, id string) (map[string]string, error) {
	return providerRevisions(ctx, id)
}
