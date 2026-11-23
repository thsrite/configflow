package discovery

import (
	"bytes"
	"configflow/internal/jsondecode"
	"context"
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

const minAgentVersion = "1.4.0-go"
const minProbeVersion = "1.5.0-go"
const minRegionVersion = "1.6.0-go"

var now = time.Now

func timestamp() string { return now().Format("2006-01-02T15:04:05") }

// Stats contains only traffic counters needed for domain discovery.
type Stats struct {
	Conns     int64            `json:"conns"`
	Fails     int64            `json:"fails"`
	ZeroDL    int64            `json:"zero_dl"`
	Up        int64            `json:"up"`
	Down      int64            `json:"down"`
	FailKinds map[string]int64 `json:"fail_kinds"`
}

func (s *Stats) add(v Stats) {
	s.Conns += v.Conns
	s.Fails += v.Fails
	s.ZeroDL += v.ZeroDL
	s.Up += v.Up
	s.Down += v.Down
	if s.FailKinds == nil {
		s.FailKinds = map[string]int64{}
	}
	for k, v := range v.FailKinds {
		s.FailKinds[k] += v
	}
}

type collectionStatus struct {
	Value         string  `json:"value"`
	At            *string `json:"at"`
	MihomoVersion string  `json:"mihomo_version"`
}
type traffic struct {
	Status       collectionStatus      `json:"status"`
	Days         map[string]trafficDay `json:"days"`
	dayOrder     []string
	storeState   string
	storeMessage string
}
type trafficDay struct {
	IPOnly    int64                  `json:"ip_only_conns"`
	Hosts     map[string]trafficHost `json:"hosts"`
	hostOrder []string
}
type trafficHost struct {
	First                 string           `json:"first_seen"`
	Last                  string           `json:"last_seen"`
	Ports                 map[string]int64 `json:"ports"`
	Routes                map[string]Stats `json:"by_route"`
	portOrder, routeOrder []string
}

// Object key order is used only to break ties in stable traffic rankings.
func fieldOrder(raw json.RawMessage) []string {
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	out := []string{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil
		}
		out = append(out, key.(string))
	}
	return out
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (t *traffic) UnmarshalJSON(b []byte) error {
	type plain traffic
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*t = traffic(p)
	var raw struct {
		Days json.RawMessage `json:"days"`
	}
	if err := jsondecode.Unmarshal(b, &raw); err != nil {
		return err
	}
	t.dayOrder = fieldOrder(raw.Days)
	return nil
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (t *trafficDay) UnmarshalJSON(b []byte) error {
	type plain trafficDay
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*t = trafficDay(p)
	var raw struct {
		Hosts json.RawMessage `json:"hosts"`
	}
	if err := jsondecode.Unmarshal(b, &raw); err != nil {
		return err
	}
	t.hostOrder = fieldOrder(raw.Hosts)
	return nil
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (t *trafficHost) UnmarshalJSON(b []byte) error {
	type plain trafficHost
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*t = trafficHost(p)
	var raw struct {
		Ports  json.RawMessage `json:"ports"`
		Routes json.RawMessage `json:"by_route"`
	}
	if err := jsondecode.Unmarshal(b, &raw); err != nil {
		return err
	}
	t.portOrder = fieldOrder(raw.Ports)
	t.routeOrder = fieldOrder(raw.Routes)
	return nil
}

// AgentStatus exposes collection status without registry credentials.
type AgentStatus struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	LastReport     *string `json:"last_report"`
	MihomoVersion  string  `json:"mihomo_version"`
	Enabled        bool    `json:"enabled"`
	Version        string  `json:"version"`
	Supported      bool    `json:"supported"`
	ProbeSupported bool    `json:"probe_supported"`
	StoreState     string  `json:"store_state"`
	StoreMessage   string  `json:"store_message"`
}

func statusOf(a Agent, data traffic) AgentStatus {
	s := data.Status
	if s.Value == "" {
		s.Value = "no_data"
	}
	storeState := data.storeState
	if storeState == "" {
		storeState = "ok"
	}
	return AgentStatus{
		ID:             a.ID,
		Name:           a.Name,
		Status:         s.Value,
		LastReport:     s.At,
		MihomoVersion:  s.MihomoVersion,
		Enabled:        a.Enabled,
		Version:        a.Version,
		Supported:      supported(a.Version, minAgentVersion),
		ProbeSupported: supported(a.Version, minProbeVersion),
		StoreState:     storeState,
		StoreMessage:   data.storeMessage,
	}
}

// RouteStats groups observations by Mihomo routing decision.
type RouteStats struct {
	Stats
	Rule        string `json:"rule"`
	RulePayload string `json:"rule_payload"`
	Outlet      string `json:"outlet"`
	Policy      string `json:"policy"`
}

// Host contains the summary and latest probe of a single observed name.
type Host struct {
	Stats
	Host        string   `json:"host"`
	First       string   `json:"first_seen"`
	Last        string   `json:"last_seen"`
	Uncovered   bool     `json:"uncovered"`
	DirectConns int64    `json:"direct_conns"`
	DirectFails int64    `json:"direct_fails"`
	AgentIDs    []string `json:"agent_ids"`
	Port        int      `json:"port"`
	Failing     bool     `json:"failing"`
	Ignored     bool     `json:"ignored"`
	Pending     *Match   `json:"pending_rule"`
	Probe       *Probe   `json:"probe"`
	portOrder   []string
	ports       map[string]int64
}

// Domain is a registrable domain and its observed hosts.
type Domain struct {
	Stats
	Domain     string        `json:"domain"`
	Tags       []string      `json:"tags"`
	Routes     []*RouteStats `json:"routes"`
	Hosts      []*Host       `json:"hosts"`
	Agents     []string      `json:"agents"`
	First      string        `json:"first_seen"`
	Last       string        `json:"last_seen"`
	Pending    *Match        `json:"pending_rule"`
	Suggestion *Suggestion   `json:"suggestion"`
	Website    Website       `json:"website"`
}

// Suggestion is a conservative rollup of the host probe recommendations.
type Suggestion struct {
	Host       string   `json:"host"`
	CheckedAt  string   `json:"checked_at"`
	Verdict    string   `json:"verdict"`
	Target     *string  `json:"target"`
	Confidence int      `json:"confidence"`
	Reasons    []string `json:"reasons"`
}

// Website combines catalog identification, cached page identity and a user note.
type Website struct {
	Service json.RawMessage `json:"service"`
	Page    *WebsitePage    `json:"page"`
	Note    string          `json:"note"`
}

// WebsitePage is persisted page metadata, with future fields preserved.
type WebsitePage struct {
	Host      string                     `json:"host"`
	CheckedAt string                     `json:"checked_at"`
	Name      string                     `json:"name"`
	Reason    string                     `json:"reason"`
	Extra     map[string]json.RawMessage `json:"-"`
}

// UnmarshalJSON decodes the known fields while retaining the original data.
func (v *WebsitePage) UnmarshalJSON(b []byte) error {
	type plain WebsitePage
	var p plain
	if err := jsondecode.Unmarshal(b, &p); err != nil {
		return err
	}
	*v = WebsitePage(p)
	return capture(b, &v.Extra, "host", "checked_at", "name", "reason")
}

// MarshalJSON preserves fields owned by other features or newer versions.
func (v WebsitePage) MarshalJSON() ([]byte, error) {
	type plain WebsitePage
	return preserve(plain(v), v.Extra)
}

// Listing is the discovery page response.
type Listing struct {
	Agents          []AgentStatus `json:"agents"`
	Coverage        *float64      `json:"sniff_coverage"`
	Items           []*Domain     `json:"items"`
	MinAgentVersion string        `json:"min_agent_version"`
	MinProbeVersion string        `json:"min_probe_agent_version"`
	Job             *Job          `json:"probe_job"`
}

func contains[T comparable](vs []T, v T) bool {
	for _, x := range vs {
		if x == v {
			return true
		}
	}
	return false
}
func ignored(domain, host string, values []string) bool {
	for _, v := range values {
		v = strings.ToLower(v)
		if host == v || domain == v || strings.HasSuffix(host, "."+v) {
			return true
		}
	}
	return false
}

type group struct {
	hostOrder, routeOrder []string
	domain                *Domain
	hosts                 map[string]*Host
	routes                map[string]*RouteStats
	agents                map[string]bool
}

func summarize(reports []struct {
	agent Agent
	data  traffic
}, days int, view, agentID string, ignore []string, match func(string) *Match) Listing {
	out := Listing{Agents: []AgentStatus{}, Items: []*Domain{}, MinAgentVersion: minAgentVersion, MinProbeVersion: minProbeVersion}
	oldest := now().AddDate(0, 0, -max(days, 1)+1).Format("2006-01-02")
	groups := map[string]*group{}
	domainOrder := []string{}
	var hostConns, ipOnly int64
	for _, r := range reports {
		a, data := r.agent, r.data
		out.Agents = append(out.Agents, statusOf(a, data))
		if agentID != "" && a.ID != agentID {
			continue
		}
		days := data.dayOrder
		for _, day := range days {
			bucket := data.Days[day]
			if day < oldest {
				continue
			}
			ipOnly += bucket.IPOnly
			for _, name := range bucket.hostOrder {
				entry := bucket.Hosts[name]
				domain := RegistrableDomain(name)
				g := groups[domain]
				if g == nil {
					g = &group{domain: &Domain{Domain: domain, Tags: []string{}, Routes: []*RouteStats{}, Hosts: []*Host{}, Agents: []string{}}, hosts: map[string]*Host{}, routes: map[string]*RouteStats{}, agents: map[string]bool{}}
					groups[domain] = g
					domainOrder = append(domainOrder, domain)
				}
				label := a.Name
				if label == "" {
					label = a.ID
				}
				g.agents[label] = true
				h := g.hosts[name]
				if h == nil {
					h = &Host{Host: name, First: entry.First, Last: entry.Last, AgentIDs: []string{}, ports: map[string]int64{}}
					g.hosts[name] = h
					g.hostOrder = append(g.hostOrder, name)
				}
				if !contains(h.AgentIDs, a.ID) {
					h.AgentIDs = append(h.AgentIDs, a.ID)
				}
				for _, port := range entry.portOrder {
					if _, ok := h.ports[port]; !ok {
						h.portOrder = append(h.portOrder, port)
					}
					h.ports[port] += entry.Ports[port]
				}
				h.First = min(h.First, entry.First)
				h.Last = max(h.Last, entry.Last)
				for _, key := range entry.routeOrder {
					s := entry.Routes[key]
					parts := strings.SplitN(key, "\t", 4)
					if len(parts) != 4 {
						continue
					}
					rs := g.routes[key]
					if rs == nil {
						rs = &RouteStats{Rule: parts[0], RulePayload: parts[1], Outlet: parts[2], Policy: parts[3]}
						g.routes[key] = rs
						g.routeOrder = append(g.routeOrder, key)
					}
					h.Stats.add(s)
					rs.Stats.add(s)
					hostConns += s.Conns
					if rs.Rule == "Match" {
						h.Uncovered = true
					}
					if rs.Outlet == "direct" {
						h.DirectConns += s.Conns
						h.DirectFails += s.Fails
					}
				}
			}
		}
	}
	for _, name := range domainOrder {
		g := groups[name]
		d := g.domain
		uncovered, pending, allIgnored, failing := 0, 0, true, false
		for _, host := range g.hostOrder {
			h := g.hosts[host]
			h.Port = 443
			weight := int64(-1)
			for _, p := range h.portOrder {
				if h.ports[p] > weight {
					weight = h.ports[p]
					if n, err := strconv.Atoi(p); err == nil {
						h.Port = n
					}
				}
			}
			h.ports = nil
			h.Failing = h.DirectFails >= 3 && h.DirectFails >= h.DirectConns
			h.Ignored = ignored(name, host, ignore)
			allIgnored = allIgnored && h.Ignored
			failing = failing || (h.Failing && !h.Ignored)
			if h.Uncovered && !h.Ignored {
				uncovered++
				if match != nil {
					h.Pending = match(host)
				}
				if h.Pending != nil {
					pending++
					if d.Pending == nil {
						d.Pending = h.Pending
					}
				}
			}
			d.Hosts = append(d.Hosts, h)
			d.Stats.add(h.Stats)
		}
		sort.SliceStable(d.Hosts, func(i, j int) bool {
			a, b := d.Hosts[i], d.Hosts[j]
			if a.Fails != b.Fails {
				return a.Fails > b.Fails
			}
			return a.Conns > b.Conns
		})
		for _, h := range d.Hosts {
			if h.Uncovered && !h.Ignored && h.Pending != nil {
				d.Pending = h.Pending
				break
			}
		}
		if uncovered > 0 {
			if pending == uncovered {
				d.Tags = append(d.Tags, "pending")
			} else {
				d.Tags = append(d.Tags, "uncovered")
			}
		}
		if failing {
			d.Tags = append(d.Tags, "failing")
		}
		if allIgnored {
			d.Tags = []string{"ignored"}
		}
		keep := false
		switch view {
		case "ignored":
			keep = allIgnored
		case "all":
			keep = !allIgnored
		case "failing":
			keep = contains(d.Tags, "failing")
		default:
			keep = contains(d.Tags, "uncovered") || contains(d.Tags, "pending")
		}
		if !keep {
			continue
		}
		for _, key := range g.routeOrder {
			d.Routes = append(d.Routes, g.routes[key])
		}
		sort.SliceStable(d.Routes, func(i, j int) bool { return d.Routes[i].Conns+d.Routes[i].Fails > d.Routes[j].Conns+d.Routes[j].Fails })
		for a := range g.agents {
			d.Agents = append(d.Agents, a)
		}
		sort.Strings(d.Agents)
		d.First = d.Hosts[0].First
		d.Last = d.Hosts[0].Last
		for _, h := range d.Hosts {
			d.First = min(d.First, h.First)
			d.Last = max(d.Last, h.Last)
		}
		out.Items = append(out.Items, d)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i], out.Items[j]
		af, bf := contains(a.Tags, "failing"), contains(b.Tags, "failing")
		if af != bf {
			return af
		}
		if a.Fails != b.Fails {
			return a.Fails > b.Fails
		}
		return a.Conns > b.Conns
	})
	if total := hostConns + ipOnly; total > 0 {
		out.Coverage = new(math.RoundToEven(float64(hostConns)/float64(total)*10000) / 10000)
	}
	return out
}
func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
func reports(ctx context.Context, as []Agent, selected string) ([]struct {
	agent Agent
	data  traffic
}, error) {
	out := []struct {
		agent Agent
		data  traffic
	}{}
	for _, a := range as {
		t, err := loadTraffic(ctx, a, selected == "" || selected == a.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, struct {
			agent Agent
			data  traffic
		}{a, t})
	}
	return out, nil
}

// AgentsPayload reads only small collection statuses, without rule indexes.
func AgentsPayload(ctx context.Context, id string) (*AgentsView, error) {
	as, err := profileAgents(ctx, id)
	if err != nil {
		return nil, err
	}
	rs, err := reports(ctx, as, "\x00")
	if err != nil {
		return nil, err
	}
	out := []AgentStatus{}
	for _, r := range rs {
		out = append(out, statusOf(r.agent, r.data))
	}
	return &AgentsView{out, minAgentVersion, minProbeVersion}, nil
}

// LoadDomains aggregates selected traffic while retaining every Agent status.
func LoadDomains(ctx context.Context, id string, days int, view, selected string) (Listing, error) {
	c, err := loadConfig(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	as, err := profileAgents(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	rs, err := reports(ctx, as, selected)
	if err != nil {
		return Listing{}, err
	}
	match, err := localMatcher(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	out := summarize(rs, days, view, selected, c.Settings.Ignored, match)
	ps, err := probes(ctx, as)
	if err != nil {
		return out, err
	}
	pages := map[string]WebsitePage{}
	oldest := now().AddDate(0, 0, -30).Format("2006-01-02T15:04:05")
	for _, a := range as {
		if selected != "" && a.ID != selected {
			continue
		}
		data, err := regionData(ctx, a.ID)
		if err != nil {
			return out, err
		}
		for host, page := range data.Websites {
			if page.CheckedAt >= oldest && page.CheckedAt > pages[host].CheckedAt {
				pages[host] = page
			}
		}
	}
	for _, d := range out.Items {
		for _, h := range d.Hosts {
			if p, ok := ps[h.Host]; ok {
				h.Probe = &p
			}
			if p, ok := pages[h.Host]; ok && (d.Website.Page == nil || p.CheckedAt > d.Website.Page.CheckedAt) {
				d.Website.Page = &p
			}
		}
		d.Suggestion = suggestion(d.Hosts)
		d.Website.Service = knownService(d.Domain)
		d.Website.Note = c.Settings.WebsiteNotes[d.Domain]
	}
	out.Job = runningJob(id)
	return out, nil
}
