package discovery

import (
	"context"
	"fmt"
)

type websitePlan struct {
	agent  Agent
	domain string
	host   *Host
}

func websiteCandidates(domain string, h *Host) []ProbeTarget {
	out := []ProbeTarget{}
	seen := map[string]bool{}
	for _, name := range []string{domain, "www." + domain, h.Host} {
		if seen[name] {
			continue
		}
		seen[name] = true
		port := 443
		if name == h.Host {
			port = h.Port
		}
		out = append(out, ProbeTarget{name, probeURL(name, port)})
	}
	return out
}

// StartWebsite identifies only domains recently collected by eligible Agents.
func StartWebsite(ctx context.Context, id string, values []string) (*Job, error) {
	values, err := cleanValues(values)
	if err != nil {
		return nil, err
	}
	if len(values) > 30 {
		return nil, invalid(msgWebsiteLimit)
	}
	c, err := loadConfig(ctx, id)
	if err != nil {
		return nil, err
	}
	as, err := profileAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	eligible := capable(as, minRegionVersion)
	if len(eligible) == 0 {
		return nil, &ProbeError{msgWebsiteAgents}
	}
	plans := []websitePlan{}
	missing := []string{}
	summaries := map[string]Listing{}
	for _, a := range eligible {
		listing, err := rawSummary(ctx, []Agent{a})
		if err != nil {
			return nil, err
		}
		summaries[a.ID] = listing
	}
	for _, v := range values {
		found := false
		for _, a := range eligible {
			for _, d := range summaries[a.ID].Items {
				if d.Domain != v {
					continue
				}
				h := d.Hosts[0]
				for _, candidate := range d.Hosts {
					if candidate.Conns > h.Conns {
						h = candidate
					}
				}
				plans = append(plans, websitePlan{a, v, h})
				found = true
				break
			}
			if found {
				break
			}
		}
		if !found {
			missing = append(missing, v)
		}
	}
	if len(plans) == 0 {
		return nil, invalid(fmt.Sprintf(msgWebsiteMissing, missing[0]))
	}
	path := "DIRECT"
	if p := proxyPath(c); p != nil && *p != "" {
		path = *p
	}
	j, _, err := startJob(ctx, id, "website", false, func(ctx context.Context, progress func(int, int)) (outcome, error) {
		out := outcome{Errors: []JobError{}}
		for _, v := range missing {
			out.Errors = append(out.Errors, JobError{Host: v, Message: msgWebsiteSkip})
		}
		progress(0, len(plans))
		for i, p := range plans {
			candidates := websiteCandidates(p.domain, p.host)
			req := RegionRequest{Target: path, TimeoutMS: 8000, Requests: []PageRequest{}}
			for j, target := range candidates {
				req.Requests = append(req.Requests, PageRequest{ID: fmt.Sprintf("website-%d", j), URL: target.URL, Markers: websiteMarkers()})
			}
			rows, err := client.Region(ctx, p.agent, req)
			if err == nil {
				var chosen WebsitePage
				at := timestamp()
				for j, target := range candidates {
					row := RegionResult{}
					for _, r := range rows {
						if r.ID == fmt.Sprintf("website-%d", j) {
							row = r
							break
						}
					}
					entry, e := pageIdentity(row, target.Host, at)
					if e != nil {
						err = e
						break
					}
					if j == 0 || entry.Name != "" {
						chosen = entry
					}
					if entry.Name != "" {
						break
					}
				}
				if err == nil {
					err = editRegion(ctx, p.agent.ID, func(data *regionRecord) error { data.Websites[p.host.Host] = chosen; return nil })
					if err == nil && chosen.Reason != "" {
						out.Errors = append(out.Errors, JobError{Host: p.host.Host, Message: chosen.Reason})
					}
				}
			}
			if err != nil {
				out.Errors = append(out.Errors, JobError{Host: p.host.Host, Message: err.Error()})
			}
			progress(i+1, len(plans))
		}
		return out, nil
	})
	return j, err
}
