package discovery

// Native collaboration between discovery, registry and health owners.
import (
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/version"
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
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
func legacySupported(text, minimum string) bool {
	return text != "" && version.CompareAgent(text, minimum) >= 0
}
func (s *Service) legacyLoadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
	store, err := s.trafficStore()
	if err != nil {
		return traffic{}, err
	}
	var out traffic
	var state trafficstore.StoreState
	if full {
		doc := store.LoadDocument(a.ID, nil)
		err = project(doc, &out)
		state = doc.StoreState()
	} else {
		snap := store.InspectStatus(a.ID)
		state = snap.State
		if len(snap.Status) > 0 {
			err = jsondecode.Unmarshal(snap.Status, &out.Status)
		}
	}
	if state == trafficstore.StoreStateDamaged {
		out.storeState = string(trafficstore.StoreStateDamaged)
		out.storeMessage = MsgStoreDamaged
	} else {
		out.storeState = string(trafficstore.StoreStateOK)
		out.storeMessage = ""
	}
	return out, err
}
func (s *Service) legacyProbes(ctx context.Context, as []Agent) (map[string]Probe, error) {
	store, err := s.probeStore()
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
func (s *Service) legacySaveProbes(ctx context.Context, id string, ps map[string]Probe) error {
	store, err := s.probeStore()
	if err != nil {
		return err
	}
	var entries model.OrderedMap[check.TimedRecord]
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
func (s *Service) legacyRegionData(ctx context.Context, id string) (regionRecord, error) {
	store, err := s.regionStore()
	var out regionRecord
	if err == nil {
		err = project(store.LoadData(id), &out)
	}
	if out.Domains == nil {
		out.Domains = map[string]json.RawMessage{}
	}
	if out.Websites == nil {
		out.Websites = map[string]WebsitePage{}
	}
	return out, err
}
func (s *Service) legacyEditRegion(ctx context.Context, id string, edit func(*regionRecord) error) error {
	store, err := s.regionStore()
	if err != nil {
		return err
	}
	return store.UpdateDataContext(ctx, id, func(data *check.RegionData) error {
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

type managerClient struct{ agents *agents.Manager }

// Probe sends a bounded domain-probe request through the Agent manager.
func (c managerClient) Probe(ctx context.Context, a Agent, p ProbeRequest) ([]ProbeRow, error) {
	if c.agents == nil {
		return nil, errors.New("discovery: agent manager is nil")
	}
	m := c.agents
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
func (c managerClient) Targets(ctx context.Context, a Agent) ([]AvailableTarget, error) {
	if c.agents == nil {
		return nil, errors.New("discovery: agent manager is nil")
	}
	m := c.agents
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
func (c managerClient) Region(ctx context.Context, a Agent, p RegionRequest) ([]RegionResult, error) {
	if c.agents == nil {
		return nil, errors.New("discovery: agent manager is nil")
	}
	m := c.agents
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
func (s *Service) legacyRegistryAgents(ctx context.Context) ([]model.Agent, error) {
	manager, err := s.agents()
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
	var entries model.OrderedMap[check.TargetState]
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

// ProviderRevisions returns provider hashes for the existing Agent traffic protocol.
func (s *Service) ProviderRevisions(ctx context.Context, id string) (map[string]string, error) {
	return s.providerRevisions(ctx, id)
}
