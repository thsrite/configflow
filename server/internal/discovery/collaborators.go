package discovery

// Transitional typed collaborators isolate modules whose own N8 steps have not
// landed. Only legacy.go knows their dynamic signatures; discovery owns no copy
// of their business rules. See progress/n8-9-discovery.md for removal owners.
import (
	"configflow/internal/model"
	"configflow/internal/rules"
	"context"
	"encoding/json"
	"fmt"
)

type collaborators struct {
	registryAgents  func(context.Context) ([]model.Agent, error)
	saveRule        func(ctx context.Context, item libraryItem) error
	validateContent func(item libraryItem) error
	cachedContent   func(ctx context.Context, item libraryItem) ([]byte, error)
	matcherOptions  func() rules.MatcherCacheOptions
	localMatcher    func(ctx context.Context, id string) (func(string) *Match, error)
	supported       func(version, minimum string) bool
	loadTraffic     func(ctx context.Context, a Agent, full bool) (traffic, error)
	probes          func(ctx context.Context, as []Agent) (map[string]Probe, error)
	saveProbes      func(ctx context.Context, id string, ps map[string]Probe) error
	judge           func(d, p *PathStats, failing, dh, ph bool) (Probe, error)
	regionData      func(ctx context.Context, id string) (regionRecord, error)
	editRegion      func(ctx context.Context, id string, edit func(*regionRecord) error) error
	knownService    func(host string) json.RawMessage
	pageIdentity    func(result RegionResult, host, at string) (WebsitePage, error)
	publicServices  func() json.RawMessage
	probeControls   func() ([]ProbeTarget, error)
	pickTargets     func(ts []AvailableTarget, preferred *string) ([]RegionTarget, error)
	exitRequest     func() (PageRequest, error)
	serviceRequests func() ([]PageRequest, error)
	domainRequest   func(id, url string) (PageRequest, error)
	exitInfo        func(rows []RegionResult) (json.RawMessage, error)
	serviceMatrix   func(rows []RegionResult) (json.RawMessage, error)
	classifyRegion  func(result *RegionResult, url string) (json.RawMessage, error)
	summarizeRegion func(names []string, per map[string]json.RawMessage) (domainVerdict, error)
	serviceDomains  func(id string) ([]string, bool)
	websiteMarkers  func() map[string]string
}

var collaboration collaborators

func saveRule(ctx context.Context, item libraryItem) error { return collaboration.saveRule(ctx, item) }

func validateContent(item libraryItem) error { return collaboration.validateContent(item) }

func cachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	data, err := collaboration.cachedContent(ctx, item)
	if err == nil && data == nil {
		data = []byte(item.Content)
	}
	return data, err
}

func localMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	return collaboration.localMatcher(ctx, id)
}

func supported(version, minimum string) bool { return collaboration.supported(version, minimum) }

func loadTraffic(ctx context.Context, a Agent, full bool) (traffic, error) {
	return collaboration.loadTraffic(ctx, a, full)
}

func probes(ctx context.Context, as []Agent) (map[string]Probe, error) {
	return collaboration.probes(ctx, as)
}

func saveProbes(ctx context.Context, id string, ps map[string]Probe) error {
	return collaboration.saveProbes(ctx, id, ps)
}

func judge(d, p *PathStats, failing, dh, ph bool) (Probe, error) {
	return collaboration.judge(d, p, failing, dh, ph)
}

func regionData(ctx context.Context, id string) (regionRecord, error) {
	return collaboration.regionData(ctx, id)
}

func editRegion(ctx context.Context, id string, edit func(*regionRecord) error) error {
	return collaboration.editRegion(ctx, id, edit)
}

func knownService(host string) json.RawMessage { return collaboration.knownService(host) }

func pageIdentity(result RegionResult, host, at string) (WebsitePage, error) {
	return collaboration.pageIdentity(result, host, at)
}

func publicServices() json.RawMessage { return collaboration.publicServices() }

func probeControls() ([]ProbeTarget, error) { return collaboration.probeControls() }

func pickTargets(ts []AvailableTarget, preferred *string) ([]RegionTarget, error) {
	return collaboration.pickTargets(ts, preferred)
}

func regionRequests(services bool, plan []ProbeTarget) ([]PageRequest, error) {
	var requests []PageRequest
	var err error
	if services {
		requests, err = collaboration.serviceRequests()
	} else {
		var request PageRequest
		request, err = collaboration.exitRequest()
		requests = []PageRequest{request}
	}
	if err != nil {
		return nil, err
	}
	for i, target := range plan {
		request, err := collaboration.domainRequest(fmt.Sprintf("domain-%d", i), target.URL)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func exitInfo(rows []RegionResult) (json.RawMessage, error) { return collaboration.exitInfo(rows) }

func serviceMatrix(rows []RegionResult) (json.RawMessage, error) {
	return collaboration.serviceMatrix(rows)
}

func classifyRegion(result *RegionResult, url string) (json.RawMessage, error) {
	return collaboration.classifyRegion(result, url)
}

func summarizeRegion(names []string, per map[string]json.RawMessage) (domainVerdict, error) {
	return collaboration.summarizeRegion(names, per)
}

func serviceDomains(id string) ([]string, bool) { return collaboration.serviceDomains(id) }

func websiteMarkers() map[string]string { return collaboration.websiteMarkers() }
