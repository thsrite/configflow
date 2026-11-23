package clients

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/clientlist"
	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/logx"
	"configflow/internal/redact"
	"configflow/internal/routerdoc"
)

// Source reports one main router's discovery capability and read outcome.
type Source struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	OK            bool     `json:"ok"`
	Error         string   `json:"error"`
	Notes         []string `json:"notes"`
	PolicyRouting *bool    `json:"policy_routing,omitempty"`
	Gateway       *string  `json:"gateway,omitempty"`
}

// ObservedDevice adds live evidence to one saved client row.
type ObservedDevice struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
	Mode string `json:"mode"`
	routerdoc.Sighting
	Discoverable bool `json:"discoverable"`
}

// DiscoveredDevice is an unsaved host with its most specific covering policy.
type DiscoveredDevice struct {
	IP string `json:"ip"`
	routerdoc.Sighting
	Network *NetworkPolicy `json:"network"`
}

// NetworkPolicy is the saved IPv4 subnet policy covering a discovered host.
type NetworkPolicy struct {
	IP   string `json:"ip"`
	Name string `json:"name"`
	Mode string `json:"mode"`
}

// Discovery is the HTTP-ready evidence payload; profile/auth fields are R17's.
type Discovery struct {
	CheckedAt  string             `json:"checked_at"`
	Sources    []Source           `json:"sources"`
	Devices    []ObservedDevice   `json:"devices"`
	Discovered []DiscoveredDevice `json:"discovered"`
	CacheMetadata
	State   string `json:"state"`
	Message string `json:"message"`
}

// DiscoveryOptions selects profile cache reuse and current infrastructure hosts.
type DiscoveryOptions struct {
	AgentHosts []string
	ProfileID  string
	Refresh    bool
	Timeout    time.Duration
	Cache      *DiscoveryCache
}

// DiscoveryService owns read collaborators. Enrich is reserved for R17's
// fixed-IP capability read; it may add Sighting.FixedIP, never fail discovery.
// GatewayHost resolves a proxy Agent ID to its current host for policy sources.
type DiscoveryService struct {
	Spawner     lockx.Spawner
	Open        func(*spec.Router) clientlist.Reader
	Enrich      func(context.Context, clientlist.Reader, *routerdoc.Reading, string)
	GatewayHost func(context.Context, string) string
}

// NewDiscoveryService constructs a service using the standard device drivers.
func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{Open: func(r *spec.Router) clientlist.Reader {
		return clientlist.Open(r)
	}}
}

var discoveryLogger = logx.Get("backend.common.client_discovery")

func (s *DiscoveryService) readOne(ctx context.Context, r *spec.Router, opts clientlist.ReadOptions) (out RouterReading) {
	defer func() {
		if recover() != nil {
			discoveryLogger.Error(messageReadFailed)
			out = RouterReading{Error: messageReadFailed}
		}
	}()
	d := s.Open(r)
	if d == nil {
		return RouterReading{Error: messageReadFailed}
	}
	defer d.Close()
	reading, err := d.ReadClients(ctx, opts)
	if err != nil {
		var deviceError *device.RouterError
		if errors.As(err, &deviceError) {
			return RouterReading{Error: deviceError.Error()}
		}
		discoveryLogger.Error(messageReadFailed)
		return RouterReading{Error: messageReadFailed}
	}
	if s.Enrich != nil {
		s.Enrich(ctx, d, reading, r.ID)
	}
	return RouterReading{Reading: reading}
}
func (s *DiscoveryService) readAll(ctx context.Context, rs []*spec.Router, priority map[string]bool, timeout time.Duration) []RouterReading {
	deadline := time.Now().Add(timeout)
	opts := clientlist.ReadOptions{Priority: priority, Deadline: deadline.Add(-2 * time.Second)}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	channels := make([]chan RouterReading, len(rs))
	for i, r := range rs {
		ch := make(chan RouterReading, 1)
		channels[i] = ch
		spawner := s.Spawner
		if spawner == nil {
			spawner = lifecycle.DefaultSpawner{}
		}
		err := spawner.Spawn(ctx, "client-discovery-router", func(work context.Context) {
			work, stop := context.WithDeadline(work, deadline)
			defer stop()
			ch <- s.readOne(work, r, opts)
		})
		if err != nil {
			ch <- RouterReading{Error: messageReadFailed}
		}
	}
	out := make([]RouterReading, len(rs))
	for i, ch := range channels {
		select {
		case out[i] = <-ch:
		case <-ctx.Done():
			select {
			case out[i] = <-ch:
			default:
				out[i] = RouterReading{Error: fmt.Sprintf(messageTimeout, timeout.Seconds())}
			}
		}
	}
	return out
}
func endpointIP(r *spec.Router) string {
	u, err := url.Parse(r.Endpoint)
	if err != nil {
		return ""
	}
	return routerdoc.IPv4(u.Hostname())
}
func clientNetwork(value string) (netip.Prefix, bool) {
	if a, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(a, a.BitLen()), true
	}
	p, err := netip.ParsePrefix(value)
	return p.Masked(), err == nil
}
func emptyEvidence(ip string) *routerdoc.Sighting { return routerdoc.NewSighting(ip) }
func savedEvidence(p netip.Prefix, valid bool, merged map[string]*routerdoc.Sighting, routersAt map[string]string, readings []*routerdoc.Reading, complete bool) *routerdoc.Sighting {
	empty := emptyEvidence("")
	if !valid || !p.Addr().Is4() {
		return empty
	}
	if p.Bits() < 32 {
		empty.Presence = "network"
		return empty
	}
	if merged == nil {
		return empty
	}
	ip := p.Addr().String()
	s := merged[ip]
	if id, ok := routersAt[ip]; ok {
		if s != nil {
			v := *s
			empty = &v
		}
		empty.Presence = "online"
		empty.Basis = "router"
		empty.RouterID = id
		return empty
	}
	if s != nil {
		return s
	}
	if !complete {
		return empty
	}
	all := true
	for _, r := range readings {
		if r.Covers(ip) {
			empty.Presence = "offline"
			empty.Basis = "absent"
			return empty
		}
		all = all && r.Complete
	}
	if all {
		empty.Basis = "outside"
	}
	return empty
}

// Discover concurrently reads all supported main routers and joins fresh saved
// data. The owner detaches request cancellation for shared cache/probe work;
// device I/O has its own bounds and Runtime shutdown still cancels it.
func (s *DiscoveryService) Discover(ctx context.Context, config json.RawMessage, opts DiscoveryOptions) (result Discovery, err error) {
	// Drivers can hide their own password, but a peer's diagnostic/hostname or
	// a saved name can reference any credential in the profile. Project only the
	// final response; cached evidence and optimistic edit inputs remain private.
	defer func() {
		if err != nil {
			return
		}
		var data []byte
		data, err = json.Marshal(result)
		if err != nil {
			return
		}
		secrets := redact.CollectCredentials(config, nil)
		err = jsondecode.Unmarshal(redact.SanitizeCredentials(json.RawMessage(data), secrets), &result)
	}()
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return Discovery{}, err
	}
	list, err := ListDevices(config)
	if err != nil {
		return Discovery{}, err
	}
	rs := []*spec.Router{}
	for _, r := range spec.RoutersOf(config) {
		if r.Type == "routeros" || r.Type == "ikuai" {
			rs = append(rs, r)
		}
	}
	result = Discovery{CheckedAt: lifecycle.ClockFrom(ctx).Now().Local().Format("2006-01-02T15:04:05-07:00"), Sources: []Source{}, Devices: []ObservedDevice{}, Discovered: []DiscoveredDevice{}}
	networks := map[string]netip.Prefix{}
	saved := map[string]bool{}
	for _, d := range list.Devices {
		p, ok := clientNetwork(d.IP)
		if ok {
			networks[d.IP] = p
			if p.Addr().Is4() && p.Bits() == 32 {
				saved[p.Addr().String()] = true
			}
		}
	}
	if len(rs) == 0 {
		if opts.Cache != nil {
			opts.Cache.Invalidate(opts.ProfileID)
		}
		result.State = "unsupported"
		result.Message = messageUnsupported
		for _, d := range list.Devices {
			p, ok := networks[d.IP]
			e := *savedEvidence(p, ok, nil, nil, nil, false)
			if len(e.FixedIP) == 0 {
				e.FixedIP = d.FixedIP
			}
			result.Devices = append(result.Devices, ObservedDevice{d.IP, d.Name, d.Mode, e, false})
		}
		return result, nil
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	load := func(work context.Context) (Snapshot, error) {
		readings := s.readAll(work, rs, saved, timeout)
		if err := work.Err(); err != nil {
			return Snapshot{}, err
		}
		return Snapshot{readings, result.CheckedAt}, nil
	}
	var snapshot Snapshot
	if opts.Cache != nil {
		snapshot, result.CacheMetadata, err = opts.Cache.Read(ctx, opts.ProfileID, rs, load, opts.Refresh)
	} else {
		snapshot, err = load(ctx)
	}
	if err != nil {
		return Discovery{}, err
	}
	result.CheckedAt = snapshot.CheckedAt
	merged := map[string]*routerdoc.Sighting{}
	excluded := map[string]bool{}
	routersAt := map[string]string{}
	done := []*routerdoc.Reading{}
	for _, host := range opts.AgentHosts {
		if ip := routerdoc.IPv4(host); ip != "" {
			excluded[ip] = true
		}
	}
	successes := 0
	for i, r := range rs {
		item := snapshot.Readings[i]
		reading := item.Reading
		source := Source{ID: r.ID, Name: r.Name, Type: r.Type, OK: reading != nil, Error: item.Error, Notes: []string{}}
		if source.Name == "" {
			source.Name = r.ID
		}
		if r.Type == "routeros" {
			policy := len(r.RosPolicyRuleSets) > 0
			source.PolicyRouting = &policy
			if policy {
				host := ""
				if s.GatewayHost != nil {
					host = routerdoc.IPv4(s.GatewayHost(ctx, r.ProxyGatewayAgentID))
				}
				source.Gateway = &host
			}
		}
		endpoint := endpointIP(r)
		if endpoint != "" {
			excluded[endpoint] = true
		}
		if reading != nil {
			successes++
			done = append(done, reading)
			source.Notes = append(source.Notes, reading.Notes...)
			if endpoint != "" {
				if _, exists := routersAt[endpoint]; !exists {
					routersAt[endpoint] = r.ID
				}
			}
			for ip := range reading.Own {
				excluded[ip] = true
				if _, exists := routersAt[ip]; !exists {
					routersAt[ip] = r.ID
				}
			}
			for ip, e := range reading.Sightings {
				e.RouterID = r.ID
				merged[ip] = routerdoc.Better(merged[ip], e)
			}
		}
		result.Sources = append(result.Sources, source)
	}
	complete := successes == len(rs)
	result.State = "ok"
	if !complete {
		if successes > 0 {
			result.State = "partial"
			result.Message = messagePartial
		} else {
			result.State = "failed"
			result.Message = messageFailed
		}
	}
	listable := func(ip string) bool {
		e := merged[ip]
		if excluded[ip] || e == nil {
			return false
		}
		if e.MAC != "" {
			return true
		}
		for _, src := range e.Sources {
			if src != "arp" {
				return true
			}
		}
		return false
	}
	for _, d := range list.Devices {
		p, ok := networks[d.IP]
		e := *savedEvidence(p, ok, merged, routersAt, done, complete)
		if len(e.FixedIP) == 0 {
			e.FixedIP = d.FixedIP
		}
		result.Devices = append(result.Devices, ObservedDevice{d.IP, d.Name, d.Mode, e, saved[d.IP] && listable(d.IP)})
	}
	covering := []Device{}
	for _, d := range list.Devices {
		p, ok := networks[d.IP]
		if ok && p.Addr().Is4() && p.Bits() < 32 && d.Mode != "" {
			covering = append(covering, d)
		}
	}
	slices.SortStableFunc(covering, func(a, b Device) int { return networks[b.IP].Bits() - networks[a.IP].Bits() })
	ips := []string{}
	for ip := range merged {
		ips = append(ips, ip)
	}
	slices.SortFunc(ips, func(a, b string) int { aa, _ := netip.ParseAddr(a); bb, _ := netip.ParseAddr(b); return aa.Compare(bb) })
	for _, ip := range ips {
		if saved[ip] || !listable(ip) {
			continue
		}
		e := merged[ip]
		var network *NetworkPolicy
		a, _ := netip.ParseAddr(ip)
		for _, d := range covering {
			if networks[d.IP].Contains(a) {
				network = &NetworkPolicy{d.IP, d.Name, d.Mode}
				break
			}
		}
		result.Discovered = append(result.Discovered, DiscoveredDevice{ip, *e, network})
	}
	return result, nil
}
