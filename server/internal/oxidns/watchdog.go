package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
)

// Local DNS liveness replies in delivered configurations (never in profile
// drafts). The Agent checks this exact first rule before probing a listener
// (backend/agents/go-agent/routes/dns_watchdog.go).

// WatchdogName is the probe name the Agent queries.
const WatchdogName = "configflow-watchdog.invalid"

// WatchdogRule is the reply rule placed first at every server entry.
func WatchdogRule() *Node { return M("matches", L("qname full:"+WatchdogName), "exec", "reject 3") }

// EnsureLocalDNSWatchdog adds the reply at each DNS server entry of a copy
// of document: sequences get the rule first, other executors a wrapper.
// Documents with includes are returned unchanged (the Agent does not probe
// them).
func EnsureLocalDNSWatchdog(document *Node) (*Node, error) {
	result := clone(document)
	if truthy(get(result, "include")) {
		return result, nil
	}
	plugins := get(result, "plugins")
	if !has(result, "plugins") {
		plugins = L()
	}
	if !isSeq(plugins) {
		return nil, errors.New("DNS watchdog requires a plugins list")
	}
	tagged := map[string]*Node{}
	for _, plugin := range plugins.Content {
		if !isMap(plugin) {
			return nil, errors.New("DNS watchdog requires plugin objects")
		}
		tag := get(plugin, "tag")
		if !truthy(tag) {
			continue // MosDNS servers need no tag
		}
		if !isStr(tag) || tagged[tag.Value] != nil {
			return nil, errors.New("DNS watchdog cannot use ambiguous plugin tags")
		}
		tagged[tag.Value] = plugin
	}
	rule := WatchdogRule()
	var output []*Node
	wrappers, protected := map[string]string{}, map[string]bool{}
	protect := func(owner *Node, key string) error {
		entry := get(owner, key)
		if isStr(entry) && strings.Contains(entry.Value, "${") {
			return nil // resolved in the core's environment; the Agent pauses probing it
		}
		if !isStr(entry) || tagged[entry.Value] == nil {
			return errors.New("DNS watchdog cannot resolve the DNS server entry")
		}
		target := tagged[entry.Value]
		if equal(get(target, "type"), S("sequence")) {
			if !protected[entry.Value] {
				args := get(target, "args")
				if !isSeq(args) {
					return errors.New("DNS watchdog requires sequence rules to be a list")
				}
				rules := append([]*Node{clone(rule)}, filter(args.Content, func(r *Node) bool { return !equal(r, rule) })...)
				set(target, "args", seqOf(rules))
				protected[entry.Value] = true
			}
			return nil
		}
		if wrappers[entry.Value] == "" {
			sum := sha256.Sum256([]byte(entry.Value))
			base := "cf_watchdog_" + hex.EncodeToString(sum[:])[:16]
			name := base
			for suffix := 1; tagged[name] != nil; suffix++ {
				name = base + "_" + strconv.Itoa(suffix)
			}
			wrapper := M("tag", name, "type", "sequence", "args", L(clone(rule), M("exec", "$"+entry.Value)))
			tagged[name] = wrapper
			wrappers[entry.Value] = name
			output = append(output, wrapper)
		}
		set(owner, key, S(wrappers[entry.Value]))
		return nil
	}
	for _, plugin := range plugins.Content {
		switch kind := str(get(plugin, "type")); kind {
		case "udp_server", "tcp_server", "quic_server", "http_server":
			args := get(plugin, "args")
			if !isMap(args) {
				return nil, errors.New("DNS watchdog requires DNS server arguments")
			}
			if kind != "http_server" {
				if err := protect(args, "entry"); err != nil {
					return nil, err
				}
				break
			}
			entries := get(args, "entries")
			if !has(args, "entries") {
				entries = L()
			}
			if !isSeq(entries) || len(filter(entries.Content, isMap)) != len(entries.Content) {
				return nil, errors.New("DNS watchdog requires HTTP DNS entries")
			}
			for _, entry := range entries.Content {
				if err := protect(entry, "exec"); err != nil {
					return nil, err
				}
			}
		}
		output = append(output, plugin)
	}
	if has(result, "plugins") {
		set(result, "plugins", seqOf(output))
	}
	return result, nil
}
