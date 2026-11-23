package discovery

import (
	"configflow/internal/lifecycle"
	"context"
	"fmt"
	"sort"
)

// PathStats is an Agent probe path's result.
type PathStats struct {
	OK     int            `json:"ok"`
	Total  int            `json:"total"`
	Delays []float64      `json:"delays"`
	Errors map[string]int `json:"errors"`
}

// Probe is the latest persisted outcome of one host probe.
type Probe struct {
	Host       string     `json:"host"`
	URL        string     `json:"url"`
	CheckedAt  string     `json:"checked_at"`
	Verdict    string     `json:"verdict"`
	Target     *string    `json:"target"`
	Confidence int        `json:"confidence"`
	Reasons    []string   `json:"reasons"`
	Direct     *PathStats `json:"direct"`
	Proxy      *PathStats `json:"proxy"`
	ProxyPath  string     `json:"proxy_path"`
}

func suggestion(hosts []*Host) *Suggestion {
	var best *Probe
	for _, h := range hosts {
		p := h.Probe
		if p != nil && (best == nil || betterProbe(*p, *best)) {
			best = p
		}
	}
	if best == nil {
		return nil
	}
	return &Suggestion{Host: best.Host, CheckedAt: best.CheckedAt, Verdict: best.Verdict, Target: best.Target, Confidence: best.Confidence, Reasons: best.Reasons}
}

// ProbeTarget is an Agent request target.
type ProbeTarget struct {
	Host string `json:"host"`
	URL  string `json:"url"`
}

// ProbeRequest is the bounded domain-probe Agent protocol.
type ProbeRequest struct {
	Targets   []ProbeTarget `json:"targets"`
	Paths     []string      `json:"paths"`
	Attempts  int           `json:"attempts"`
	TimeoutMS int           `json:"timeout_ms"`
}

// ProbeRow contains the path results returned by an Agent.
type ProbeRow struct {
	Host  string                `json:"host"`
	Paths map[string]*PathStats `json:"paths"`
}

// AgentClient is the device I/O boundary; implementations must bound requests.
type AgentClient interface {
	Probe(context.Context, Agent, ProbeRequest) ([]ProbeRow, error)
	Targets(context.Context, Agent) ([]AvailableTarget, error)
	Region(context.Context, Agent, RegionRequest) ([]RegionResult, error)
}

var client AgentClient = managerClient{}

type plannedHost struct {
	ProbeTarget
	AgentID string
	Failing bool
}

func probeURL(host string, port int) string {
	if port == 80 {
		return "http://" + host + "/"
	}
	if port == 0 || port == 443 {
		return "https://" + host + "/"
	}
	return fmt.Sprintf("https://%s:%d/", host, port)
}
func capable(as []Agent, minimum string) []Agent {
	out := []Agent{}
	for _, a := range as {
		if a.Enabled && supported(a.Version, minimum) {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Heartbeat > out[j].Heartbeat })
	return out
}
func rawSummary(ctx context.Context, as []Agent) (Listing, error) {
	rs, err := reports(ctx, as, "")
	if err != nil {
		return Listing{}, err
	}
	return summarize(rs, 7, "all", "", nil, nil), nil
}
func probePlan(ctx context.Context, id string, values []string) (map[string]Agent, []plannedHost, error) {
	as, err := profileAgents(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	eligible := capable(as, minProbeVersion)
	if len(eligible) == 0 {
		return nil, nil, &ProbeError{msgProbeAgents}
	}
	listing, err := rawSummary(ctx, as)
	if err != nil {
		return nil, nil, err
	}
	byAgent := map[string]Agent{}
	for _, a := range eligible {
		byAgent[a.ID] = a
	}
	domains := map[string]*Domain{}
	hosts := map[string]*Host{}
	for _, d := range listing.Items {
		domains[d.Domain] = d
		for _, h := range d.Hosts {
			hosts[h.Host] = h
		}
	}
	plan := []plannedHost{}
	seen := map[string]bool{}
	for _, v := range values {
		candidates := []*Host{}
		if d := domains[v]; d != nil {
			candidates = d.Hosts[:min(3, len(d.Hosts))]
		} else if h := hosts[v]; h != nil {
			candidates = append(candidates, h)
		} else {
			candidates = append(candidates, &Host{Host: v, Port: 443})
		}
		for _, h := range candidates {
			if seen[h.Host] {
				continue
			}
			seen[h.Host] = true
			aid := eligible[0].ID
			for _, candidate := range h.AgentIDs {
				if _, ok := byAgent[candidate]; ok {
					aid = candidate
					break
				}
			}
			plan = append(plan, plannedHost{ProbeTarget{h.Host, probeURL(h.Host, h.Port)}, aid, h.Failing})
		}
	}
	return byAgent, plan, nil
}
func healthy(s *PathStats) bool {
	return s != nil && s.Total > 0 && float64(s.OK)/float64(s.Total) >= 0.5
}
func runProbes(ctx context.Context, id string, values []string, progress func(int, int)) (outcome, error) {
	out := outcome{Results: map[string]Probe{}, Errors: []JobError{}}
	c, err := loadConfig(ctx, id)
	if err != nil {
		return out, err
	}
	path := proxyPath(c)
	if path == nil || *path == "" {
		return out, &ProbeError{msgProbePath}
	}
	as, plan, err := probePlan(ctx, id, values)
	if err != nil {
		return out, err
	}
	progress(0, len(plan))
	done := 0
	for _, aid := range sortedKeys(as) {
		targets := []plannedHost{}
		for _, h := range plan {
			if h.AgentID == aid {
				targets = append(targets, h)
			}
		}
		for start := 0; start < len(targets); start += 18 {
			batch := targets[start:min(start+18, len(targets))]
			controls, err := probeControls()
			if err != nil {
				return out, err
			}
			req := ProbeRequest{Targets: controls, Paths: []string{"DIRECT", *path}, Attempts: 3, TimeoutMS: 5000}
			for _, h := range batch {
				req.Targets = append(req.Targets, h.ProbeTarget)
			}
			rows, err := client.Probe(ctx, as[aid], req)
			if err != nil {
				out.Errors = append(out.Errors, JobError{AgentID: aid, Message: err.Error()})
			} else if len(rows) != len(req.Targets) {
				out.Errors = append(out.Errors, JobError{AgentID: aid, Message: msgResultCount})
			} else {
				dh, ph := healthy(rows[0].Paths["DIRECT"]), healthy(rows[1].Paths[*path])
				saved := map[string]Probe{}
				for i, h := range batch {
					d, p := rows[i+2].Paths["DIRECT"], rows[i+2].Paths[*path]
					v, err := judge(d, p, h.Failing, dh, ph)
					if err != nil {
						return out, err
					}
					v.Host = h.Host
					v.URL = h.URL
					v.CheckedAt = timestamp()
					v.Direct = d
					v.Proxy = p
					v.ProxyPath = *path
					saved[h.Host] = v
					out.Results[h.Host] = v
				}
				if err := saveProbes(ctx, aid, saved); err != nil {
					return out, err
				}
			}
			done += len(batch)
			progress(done, len(plan))
		}
	}
	return out, nil
}

// StartProbe starts an exclusive asynchronous probe job.
func StartProbe(ctx context.Context, id string, values []string) (*Job, error) {
	ctx = lifecycle.Detach(ctx)
	values, err := cleanValues(values)
	if err != nil {
		return nil, err
	}
	j, _, err := startJob(ctx, id, "probe", false, func(ctx context.Context, p func(int, int)) (outcome, error) { return runProbes(ctx, id, values, p) })
	return j, err
}
