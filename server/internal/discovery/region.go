package discovery

import (
	"context"
	"encoding/json"
	"fmt"
)

// AvailableTarget is an Agent's usable proxy target.
type AvailableTarget struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Alive *bool  `json:"alive,omitempty"`
}

// RegionTarget is a selected direct path, proxy group or regional node.
type RegionTarget struct {
	Name       string  `json:"name"`
	Kind       string  `json:"kind"`
	RegionHint *string `json:"region_hint"`
}

// RegionRequest carries at most twelve bounded page checks.
type RegionRequest struct {
	Target    string        `json:"target"`
	TimeoutMS int           `json:"timeout_ms"`
	Requests  []PageRequest `json:"requests"`
}

// PageRequest describes the markers an Agent should extract from a page.
type PageRequest struct {
	ID      string                     `json:"id"`
	URL     string                     `json:"url"`
	Markers map[string]string          `json:"markers,omitempty"`
	Extra   map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *PageRequest) UnmarshalJSON(b []byte) error {
	type plain PageRequest
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = PageRequest(p)
	return capture(b, &v.Extra, "id", "url", "markers")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v PageRequest) MarshalJSON() ([]byte, error) {
	type plain PageRequest
	return preserve(plain(v), v.Extra)
}

// RegionResult is an Agent's page response; marker payloads remain native JSON.
type RegionResult struct {
	ID       string                     `json:"id"`
	Status   *int                       `json:"status"`
	Error    string                     `json:"error,omitempty"`
	FinalURL string                     `json:"final_url,omitempty"`
	Markers  map[string]json.RawMessage `json:"markers"`
	Extra    map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *RegionResult) UnmarshalJSON(b []byte) error {
	type plain RegionResult
	p := plain{Markers: map[string]json.RawMessage{}}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = RegionResult(p)
	return capture(b, &v.Extra, "id", "status", "error", "final_url", "markers")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v RegionResult) MarshalJSON() ([]byte, error) {
	type plain RegionResult
	return preserve(plain(v), v.Extra)
}

type regionRecord struct {
	Services json.RawMessage            `json:"services"`
	Domains  map[string]json.RawMessage `json:"domains"`
	Websites map[string]WebsitePage     `json:"websites"`
}

type targetInfo struct {
	RegionTarget
	Exit  json.RawMessage `json:"exit"`
	Error *string         `json:"error"`
}
type domainVerdict struct {
	Verdict    string   `json:"verdict"`
	Restricted []string `json:"restricted"`
	Usable     []string `json:"usable"`
}
type regionDomain struct {
	domainVerdict
	CheckedAt   string                     `json:"checked_at"`
	URL         string                     `json:"url"`
	Targets     map[string]json.RawMessage `json:"targets"`
	TargetInfos []targetInfo               `json:"target_infos"`
}

func evaluateRegion(targets []RegionTarget, raw map[string][]RegionResult, plan []ProbeTarget, services bool, failures []JobError, at string) (regionRecord, error) {
	out := regionRecord{Domains: map[string]json.RawMessage{}}
	infos := []targetInfo{}
	matrix := map[string]json.RawMessage{}
	names := []string{}
	for _, target := range targets {
		names = append(names, target.Name)
		info := targetInfo{RegionTarget: target}
		rows := raw[target.Name]
		if len(rows) > 0 {
			exit, err := exitInfo(rows)
			if err != nil {
				return out, err
			}
			info.Exit = exit
			if services {
				value, err := serviceMatrix(rows)
				if err != nil {
					return out, err
				}
				matrix[target.Name] = value
			}
		}
		for _, failure := range failures {
			if failure.Target == target.Name {
				info.Error = ptr(failure.Message)
				break
			}
		}
		infos = append(infos, info)
	}
	if services {
		b, err := json.Marshal(struct {
			CheckedAt string                     `json:"checked_at"`
			Targets   []targetInfo               `json:"targets"`
			Matrix    map[string]json.RawMessage `json:"matrix"`
		}{at, infos, matrix})
		if err != nil {
			return out, err
		}
		out.Services = b
	}
	for i, host := range plan {
		per := map[string]json.RawMessage{}
		for _, name := range names {
			rows := raw[name]
			if len(rows) == 0 {
				continue
			}
			var result *RegionResult
			for j := range rows {
				if rows[j].ID == fmt.Sprintf("domain-%d", i) {
					result = &rows[j]
					break
				}
			}
			value, err := classifyRegion(result, host.URL)
			if err != nil {
				return out, err
			}
			per[name] = value
		}
		verdict, err := summarizeRegion(names, per)
		if err != nil {
			return out, err
		}
		b, err := json.Marshal(regionDomain{verdict, at, host.URL, per, infos})
		if err != nil {
			return out, err
		}
		out.Domains[host.Host] = b
	}
	return out, nil
}

func regionPlan(list Listing, values []string) []ProbeTarget {
	out := []ProbeTarget{}
	seen := map[string]bool{}
	for _, v := range values {
		h := &Host{Host: v, Port: 443}
		for _, d := range list.Items {
			if d.Domain == v {
				h = d.Hosts[0]
				break
			}
			for _, host := range d.Hosts {
				if host.Host == v {
					h = host
				}
			}
		}
		if !seen[h.Host] {
			out = append(out, ProbeTarget{h.Host, probeURL(h.Host, h.Port)})
			seen[h.Host] = true
		}
	}
	return out
}
func runRegion(ctx context.Context, id string, services bool, values []string, progress func(int, int)) (outcome, error) {
	out := outcome{Errors: []JobError{}}
	c, err := loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	if !c.Settings.RegionEnabled {
		return out, &ProbeError{msgRegionEnabled}
	}
	as, err := profileAgents(ctx, id)
	if err != nil {
		return out, err
	}
	eligible := capable(as, minRegionVersion)
	if len(eligible) == 0 {
		return out, &ProbeError{msgRegionAgents}
	}
	a := eligible[0]
	available, err := client.Targets(ctx, a)
	if err != nil {
		return out, &ProbeError{err.Error()}
	}
	targets, err := pickTargets(available, proxyPath(c))
	if err != nil {
		return out, err
	}
	listing, err := rawSummary(ctx, as)
	if err != nil {
		return out, err
	}
	plan := regionPlan(listing, values)
	requests, err := regionRequests(services, plan)
	if err != nil {
		return out, err
	}
	raw := map[string][]RegionResult{}
	progress(0, len(targets))
	for i, t := range targets {
		results := []RegionResult{}
		for start := 0; start < len(requests); start += 12 {
			res, err := client.Region(ctx, a, RegionRequest{t.Name, 8000, requests[start:min(start+12, len(requests))]})
			if err != nil {
				out.Errors = append(out.Errors, JobError{Target: t.Name, Message: err.Error()})
				break
			}
			results = append(results, res...)
		}
		raw[t.Name] = results
		progress(i+1, len(targets))
	}
	payload, err := evaluateRegion(targets, raw, plan, services, out.Errors, timestamp())
	if err != nil {
		return out, err
	}
	err = editRegion(ctx, a.ID, func(data *regionRecord) error {
		if services {
			data.Services = payload.Services
		}
		for k, v := range payload.Domains {
			data.Domains[k] = v
		}
		return nil
	})
	return out, err
}

// StartRegion starts an exclusive service/domain region check.
func StartRegion(ctx context.Context, id string, services bool, values []string) (*Job, error) {
	var err error
	if len(values) > 0 {
		values, err = cleanValues(values)
		if err != nil {
			return nil, err
		}
		if len(values) > 10 {
			return nil, invalid(fmt.Sprintf(msgRegionLimit, 10))
		}
	}
	if !services && len(values) == 0 {
		return nil, invalid(msgRegionSelect)
	}
	j, _, err := startJob(ctx, id, "region", false, func(ctx context.Context, p func(int, int)) (outcome, error) {
		return runRegion(ctx, id, services, values, p)
	})
	return j, err
}

// RegionPayload merges the newest persisted results of the current profile.
func RegionPayload(ctx context.Context, id string) (map[string]any, error) {
	c, err := loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	as, err := profileAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	var chosen *Agent
	var result json.RawMessage
	latest := ""
	domains := map[string]json.RawMessage{}
	for i, a := range as {
		data, err := regionData(ctx, a.ID)
		if err != nil {
			return nil, err
		}
		at := checkedAt(data.Services)
		if i == 0 || (len(data.Services) > 0 && string(data.Services) != "null" && at > latest) {
			chosen = ptr(a)
			result = data.Services
			latest = at
		}
		for host, entry := range data.Domains {
			if old, ok := domains[host]; !ok || checkedAt(entry) > checkedAt(old) {
				domains[host] = entry
			}
		}
	}
	var agent any
	if chosen != nil {
		agent = map[string]string{"id": chosen.ID, "name": chosen.Name}
	}
	regional := map[string]*RulesetInfo{}
	for policy, rid := range c.Settings.RegionalRulesets {
		regional[policy] = info(c, ptr(rid))
	}
	return map[string]any{"enabled": c.Settings.RegionEnabled, "port": c.Settings.RegionPort, "services": publicServices(), "result": result, "agent": agent, "domains": domains, "policies": c.policies(), "ready": len(capable(as, minRegionVersion)) > 0, "min_agent_version": minRegionVersion, "job": runningJob(id), "regional_rulesets": regional}, nil
}
func checkedAt(raw json.RawMessage) string {
	var v struct {
		At string `json:"checked_at"`
	}
	_ = json.Unmarshal(raw, &v)
	return v.At
}

// RouteToPolicy prepares a regional ruleset ahead of competing rules and adopts domains.
func RouteToPolicy(ctx context.Context, id, kind, value, policy string) (ApplyResult, error) {
	var values []string
	switch kind {
	case "service":
		var ok bool
		values, ok = serviceDomains(value)
		if !ok {
			return ApplyResult{}, invalid(msgService)
		}
	case "domain":
		v := CleanDomain(value)
		if v == "" {
			return ApplyResult{}, invalid(msgDomain)
		}
		values = []string{v}
	default:
		return ApplyResult{}, invalid(msgKind)
	}
	if err := ensurePolicy(ctx, id, policy, values); err != nil {
		return ApplyResult{}, err
	}
	entries := []ApplyEntry{}
	for _, v := range values {
		entries = append(entries, ApplyEntry{v, "DOMAIN-SUFFIX", "policy:" + policy})
	}
	return ApplyDomains(ctx, id, entries, "region", nil)
}
func ensurePolicy(ctx context.Context, id, policy string, values []string) error {
	c, err := loadConfig(ctx, id)
	if err != nil {
		return err
	}
	if !contains(c.policies(), policy) {
		return invalid(msgPolicy)
	}
	inf := info(c, ptr(c.Settings.RegionalRulesets[policy]))
	if inf != nil && inf.Problem == nil && inf.Active {
		return nil
	}
	m, err := localMatcher(ctx, id)
	if err != nil {
		return err
	}
	competing := map[string]bool{}
	for _, v := range values {
		if match := m(v); match != nil {
			competing[match.RuleID] = true
		}
	}
	rid := ""
	if inf != nil && inf.Problem == nil {
		rid = inf.ID
	} else {
		var created libraryItem
		err = editLibrary(ctx, func(items *[]libraryItem) error {
			created = newLibrary(*items, fmt.Sprintf(msgPolicyName, policy))
			*items = append(*items, created)
			return nil
		})
		if err != nil {
			return err
		}
		if err = saveRule(ctx, created); err != nil {
			return err
		}
		rid = created.ID
	}
	return editProfile(ctx, id, func(c *config) error {
		exists := false
		for _, r := range c.Rules {
			exists = exists || r.ItemType == "ruleset" && r.LibraryID == rid && r.Enabled
		}
		if !exists {
			pos := len(c.Rules)
			for i, r := range c.Rules {
				if competing[r.ID] || (r.ItemType == "ruleset" && c.Settings.ProxyRuleset != nil && r.LibraryID == *c.Settings.ProxyRuleset) || (r.ItemType == "rule" && r.RuleType == "MATCH") {
					pos = i
					break
				}
			}
			ref, err := newReference(rid, policy)
			if err != nil {
				return err
			}
			c.Rules = insertRules(c.Rules, pos, ref)
		}
		c.Settings.RegionalRulesets[policy] = rid
		return nil
	})
}
