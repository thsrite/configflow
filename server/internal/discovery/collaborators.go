package discovery

import (
	"context"
	"encoding/json"
	"fmt"
)

func (s *Service) saveRule(ctx context.Context, item libraryItem) error {
	return s.nativeSaveRule(ctx, item)
}

func validateContent(item libraryItem) error { return nativeValidateContent(item) }

func (s *Service) cachedContent(ctx context.Context, item libraryItem) ([]byte, error) {
	data, err := s.nativeCachedContent(ctx, item)
	if err == nil && data == nil {
		data = []byte(item.Content)
	}
	return data, err
}

func (s *Service) localMatcher(ctx context.Context, id string) (func(string) *Match, error) {
	return s.nativeLocalMatcher(ctx, id)
}

func supported(version, minimum string) bool { return legacySupported(version, minimum) }

func (s *Service) probes(ctx context.Context, as []Agent) (map[string]Probe, error) {
	return s.legacyProbes(ctx, as)
}

func (s *Service) saveProbes(ctx context.Context, id string, ps map[string]Probe) error {
	return s.legacySaveProbes(ctx, id, ps)
}

func judge(d, p *PathStats, failing, dh, ph bool) (Probe, error) {
	return legacyJudge(d, p, failing, dh, ph)
}

func (s *Service) regionData(ctx context.Context, id string) (regionRecord, error) {
	return s.legacyRegionData(ctx, id)
}

func (s *Service) editRegion(ctx context.Context, id string, edit func(*regionRecord) error) error {
	return s.legacyEditRegion(ctx, id, edit)
}

func knownService(host string) json.RawMessage { return nativeKnownService(host) }

func pageIdentity(result RegionResult, host, at string) (WebsitePage, error) {
	return nativePageIdentity(result, host, at)
}

func publicServices() json.RawMessage { return legacyPublicServices() }

func probeControls() ([]ProbeTarget, error) { return legacyProbeControls() }

func pickTargets(ts []AvailableTarget, preferred *string) ([]RegionTarget, error) {
	return legacyPickTargets(ts, preferred)
}

func regionRequests(services bool, plan []ProbeTarget) ([]PageRequest, error) {
	var requests []PageRequest
	var err error
	if services {
		requests, err = legacyServiceRequests()
	} else {
		var request PageRequest
		request, err = legacyExitRequest()
		requests = []PageRequest{request}
	}
	if err != nil {
		return nil, err
	}
	for i, target := range plan {
		request, err := legacyDomainRequest(fmt.Sprintf("domain-%d", i), target.URL)
		if err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, nil
}

func exitInfo(rows []RegionResult) (json.RawMessage, error) { return legacyExitInfo(rows) }

func serviceMatrix(rows []RegionResult) (json.RawMessage, error) {
	return legacyServiceMatrix(rows)
}

func classifyRegion(result *RegionResult, url string) (json.RawMessage, error) {
	return legacyClassifyRegion(result, url)
}

func summarizeRegion(names []string, per map[string]json.RawMessage) (domainVerdict, error) {
	return legacySummarizeRegion(names, per)
}

func serviceDomains(id string) ([]string, bool) { return legacyServiceDomains(id) }

func websiteMarkers() map[string]string { return legacyWebsiteMarkers() }
