package dnsfollow

import (
	"bytes"
	"configflow/internal/jsondecode"
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type upstream struct {
	tag   string
	index int
	entry *yamlx.Node
}

func text(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		return n.Value
	}
	return ""
}
func elements(n *yamlx.Node) []*yamlx.Node {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.SequenceNode {
		return n.Content
	}
	return nil
}

func document(src *deploy.Source, baseURL string) (*yamlx.Node, *yamlx.Node, error) {
	raw, err := src.Raw()
	if err != nil {
		return nil, nil, err
	}
	p, err := oxidns.ProfileFromJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	settings, err := oxidns.PrepareEditor(p.Settings(), p, baseURL)
	if err != nil {
		return nil, nil, err
	}
	doc, err := oxidns.LoadNativeYAML(text(yamlx.Get(settings, "native_config")), "")
	return settings, doc, err
}
func entries(doc *yamlx.Node) []upstream {
	plugins := elements(yamlx.Get(doc, "plugins"))
	counts := map[string]int{}
	for _, p := range plugins {
		counts[text(yamlx.Get(p, "tag"))]++
	}
	var out []upstream
	for _, p := range plugins {
		tag := text(yamlx.Get(p, "tag"))
		if tag == "" || counts[tag] != 1 || strings.HasPrefix(tag, "cf_dns_") || text(yamlx.Get(p, "type")) != "forward" {
			continue
		}
		for i, e := range elements(yamlx.Get(yamlx.Get(p, "args"), "upstreams")) {
			addr := yamlx.Get(e, "addr")
			if addr != nil && yamlx.Resolve(addr).Tag == "!!str" {
				out = append(out, upstream{tag, i, e})
			}
		}
	}
	return out
}
func boundEntry(doc *yamlx.Node, b *model.DNSFollowBinding) (*yamlx.Node, error) {
	// Stored R5 fallback values are not selection evidence. In particular a
	// wrong-type or null index must never become a claim to upstream zero.
	raw, err := json.Marshal(b)
	var selected struct {
		Tag   *string `json:"upstream_tag"`
		Index *int64  `json:"upstream_index"`
	}
	if err != nil || jsondecode.Unmarshal(raw, &selected) != nil || selected.Tag == nil || selected.Index == nil || *selected.Index < 0 {
		return nil, refusal(msgRemoved)
	}
	for _, e := range entries(doc) {
		if e.tag == b.UpstreamTag && int64(e.index) == int64(b.UpstreamIndex) {
			return e.entry, nil
		}
	}
	return nil, refusal(msgRemoved)
}
func scheme(address string) (string, error) {
	if !strings.Contains(address, "://") {
		address = "udp://" + address
	}
	u, err := url.Parse(address)
	if err != nil || u == nil {
		return "", refusal(msgScheme)
	}
	credentials := false
	if u.User != nil {
		password, _ := u.User.Password()
		credentials = u.User.Username() != "" || password != ""
	}
	s := strings.ToLower(u.Scheme)
	if (s != "udp" && s != "tcp") || u.Hostname() == "" || credentials || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", refusal(msgScheme)
	}
	return s, nil
}

var hostPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?$`)

func networkHost(host string) (string, error) {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return "", refusal(msgHost)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
			return "", refusal(msgHostLocal)
		}
		return host, nil
	}
	if !hostPattern.MatchString(host) || strings.EqualFold(host, "localhost") {
		return "", refusal(msgHostFormat)
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", refusal(msgHostFormat)
		}
	}
	return host, nil
}
func suggestedHost(host string) string {
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		ip, err := netip.ParseAddr(host[1 : len(host)-1])
		if err != nil || !ip.Is6() {
			return ""
		}
		host = ip.String()
	}
	result, _ := networkHost(host)
	return result
}
func pinnedRule(src *deploy.Source, settings *yamlx.Node, address string) int {
	ids := map[string]bool{}
	for _, key := range []string{"rule_actions", "rule_dns_parked"} {
		for id, action := range yamlx.Pairs(yamlx.Get(settings, key)) {
			if text(yamlx.Get(action, "action")) == "upstream" && text(yamlx.Get(yamlx.Get(action, "upstream"), "addr")) == address {
				ids[id] = true
			}
		}
	}
	var rows []struct {
		ID string `json:"id"`
	}
	_ = jsondecode.Unmarshal(src.Member("rule_configs"), &rows)
	for i, row := range rows {
		if ids[row.ID] {
			return i + 1
		}
	}
	return 0
}
func signature(node *yamlx.Node) (string, error) {
	raw, err := yamlx.ToJSON(node)
	if err != nil {
		return "", err
	}
	return deploy.CanonicalDigest(raw), nil
}
func requestBinding(raw json.RawMessage) (model.Fields, error) {
	var fields model.Fields
	if json.Unmarshal(raw, &fields) != nil {
		return fields, refusal(msgParameters)
	}
	for _, key := range fields.Keys() {
		switch key {
		case "source_agent_id", "upstream_tag", "upstream_index", "connection", "address":
		default:
			return fields, refusal(msgParameters)
		}
	}
	return fields, nil
}
func stringMember(f model.Fields, key string) string {
	raw, _ := f.Get(key)
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}
func selection(f model.Fields) (*model.DNSFollowBinding, error) {
	var index int64
	raw, ok := f.Get("upstream_index")
	tagRaw, tagPresent := f.Get("upstream_tag")
	var tag string
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, &index) != nil || index < 0 || !tagPresent || bytes.Equal(bytes.TrimSpace(tagRaw), []byte("null")) || json.Unmarshal(tagRaw, &tag) != nil {
		return nil, refusal(msgSelection)
	}
	connection := stringMember(f, "connection")
	if connection != "auto" && connection != "network" {
		return nil, refusal(msgSelection)
	}
	var b model.DNSFollowBinding
	// These zero/null members are explicit parts of the persisted contract.
	if err := json.Unmarshal([]byte(`{"upstream_index":0,"after_deployment_id":null}`), &b); err != nil {
		return nil, err
	}
	b.SourceAgentID = stringMember(f, "source_agent_id")
	b.UpstreamTag = tag
	b.UpstreamIndex = model.Int(index)
	b.Connection = connection
	if connection == "network" {
		host, err := networkHost(stringMember(f, "address"))
		if err != nil {
			return nil, err
		}
		b.Address = &host
	}
	return &b, nil
}
func mutable(a *agent) error {
	if activeHA(a) {
		return refusal(msgHAActive)
	}
	// A damaged stored state (no status) locks the binding like a run does
	// (O18 T1b review); the round itself reconciles it.
	if a.DNSFollowState.Busy() {
		return refusal(msgRunning)
	}
	return nil
}
func pinnedError(index int) error { return refusal(fmt.Sprintf(msgPinned, index)) }
