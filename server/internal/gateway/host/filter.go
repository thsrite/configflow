package host

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// ForwardFilter contains only the Agent report fields safe for the command UI.
type ForwardFilter struct {
	Protocol         json.Number    `json:"protocol"`
	State            string         `json:"state"`
	Reason           string         `json:"reason"`
	Device           string         `json:"device"`
	AutoRedirect     bool           `json:"auto_redirect"`
	Policy           string         `json:"policy"`
	InputPolicy      string         `json:"input_policy"`
	Flows            []string       `json:"flows"`
	ForwardProtocols []string       `json:"forward_protocols"`
	Sources          []FilterSource `json:"sources"`
	Remedies         []FilterRemedy `json:"remedies"`
	Hint             string         `json:"hint"`
	Verify           string         `json:"verify"`
	CheckedAt        string         `json:"checked_at"`
	Cached           bool           `json:"cached"`
}

// FilterSource identifies one known netfilter source without arbitrary commands.
type FilterSource struct {
	Kind    string `json:"kind"`
	Backend string `json:"backend"`
	Hook    string `json:"hook"`
	Family  string `json:"family"`
	Table   string `json:"table"`
	Chain   string `json:"chain"`
	Policy  string `json:"policy"`
	Detail  string `json:"detail"`
}

// FilterRemedy carries a bounded list of whitelisted firewall commands.
type FilterRemedy struct {
	Source      string   `json:"source"`
	Label       string   `json:"label"`
	Device      string   `json:"device"`
	Commands    []string `json:"commands"`
	PersistNote string   `json:"persist_note"`
}

var integerPattern = regexp.MustCompile(`^-?[0-9]+$`)
var nftName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,63}$`)
var reasonPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
var commandPattern = regexp.MustCompile(`^(?:iptables|iptables-legacy|nft|firewall-cmd|ufw)( [A-Za-z0-9_.:=/,+"'-]+)+$`)

const ufwRule = "-A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT"
const ufwBefore = "grep -q -- '" + ufwRule + "' /etc/ufw/before.rules || sed -i '/^# End required lines/a " + ufwRule + "' /etc/ufw/before.rules"

func cleanText(raw json.RawMessage, limit int) string {
	s := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) {
			return -1
		}
		return r
	}, text(raw))
	r := []rune(strings.TrimSpace(s))
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}
func choice(raw json.RawMessage, allowed ...string) string {
	s := text(raw)
	if slices.Contains(allowed, s) {
		return s
	}
	return ""
}
func matching(raw json.RawMessage, pattern *regexp.Regexp) string {
	s := text(raw)
	if pattern.MatchString(s) {
		return s
	}
	return ""
}
func sourceKind(raw json.RawMessage) string {
	return choice(raw, "docker", "iptables", "nftables", "firewalld", "ufw")
}
func command(raw json.RawMessage) string {
	s := text(raw)
	if s == "" || len([]rune(s)) > 240 {
		return ""
	}
	if s == ufwBefore {
		return s
	}
	for _, pair := range [][2]string{{"zone", "configflow-tun"}, {"policy", "configflow-fwd"}} {
		if s == "firewall-cmd --permanent --info-"+pair[0]+"="+pair[1]+" >/dev/null 2>&1 || firewall-cmd --permanent --new-"+pair[0]+"="+pair[1] {
			return s
		}
	}
	for _, step := range strings.Split(s, " && ") {
		if !commandPattern.MatchString(step) {
			return ""
		}
	}
	return s
}
func rawList(raw json.RawMessage) []json.RawMessage {
	var v []json.RawMessage
	_ = json.Unmarshal(raw, &v)
	return v
}
func selected(raw json.RawMessage, order ...string) []string {
	var values []string
	_ = json.Unmarshal(raw, &values)
	out := []string{}
	for _, s := range order {
		if slices.Contains(values, s) {
			out = append(out, s)
		}
	}
	return out
}

// SanitizeForwardFilter drops unknown fields, command syntax and unusable reports.
func SanitizeForwardFilter(raw json.RawMessage) *ForwardFilter {
	o := object(raw)
	state := choice(o["state"], "ok", "may_drop", "unknown", "not_applicable")
	if state == "" {
		return nil
	}
	f := &ForwardFilter{Protocol: "0", State: state, Reason: matching(o["reason"], reasonPattern), Device: matching(o["device"], devicePattern), AutoRedirect: yes(o["auto_redirect"]), Policy: choice(o["policy"], "drop", "accept"), InputPolicy: choice(o["input_policy"], "drop", "accept"), Flows: selected(o["flows"], "forward", "dns", "local_dns", "redirect_tcp", "tun_tcp"), ForwardProtocols: []string{}, Sources: []FilterSource{}, Remedies: []FilterRemedy{}, Hint: cleanText(o["hint"], 400), Verify: cleanText(o["verify"], 300), CheckedAt: cleanText(o["checked_at"], 40), Cached: yes(o["cached"])}
	if integerPattern.Match(o["protocol"]) {
		f.Protocol = json.Number(o["protocol"])
	}
	if slices.Contains(f.Flows, "forward") {
		f.ForwardProtocols = selected(o["forward_protocols"], "tcp", "udp", "icmp")
	}
	for _, raw := range rawList(o["sources"]) {
		s := object(raw)
		kind := sourceKind(s["kind"])
		if kind == "" {
			continue
		}
		f.Sources = append(f.Sources, FilterSource{kind, choice(s["backend"], "nft", "legacy"), choice(s["hook"], "forward", "input"), choice(s["family"], "ip", "inet"), matching(s["table"], nftName), matching(s["chain"], nftName), choice(s["policy"], "drop", "accept"), cleanText(s["detail"], 200)})
		if len(f.Sources) == 8 {
			break
		}
	}
	for _, raw := range rawList(o["remedies"]) {
		r := object(raw)
		kind := sourceKind(r["source"])
		if kind == "" {
			continue
		}
		remedy := FilterRemedy{Source: kind, Label: cleanText(r["label"], 80), Device: matching(r["device"], devicePattern), Commands: []string{}, PersistNote: cleanText(r["persist_note"], 300)}
		if remedy.Device != "" {
			for _, raw := range rawList(r["commands"]) {
				if s := command(raw); s != "" {
					remedy.Commands = append(remedy.Commands, s)
					if len(remedy.Commands) == 12 {
						break
					}
				}
			}
		}
		f.Remedies = append(f.Remedies, remedy)
		if len(f.Remedies) == 6 {
			break
		}
	}
	return f
}
