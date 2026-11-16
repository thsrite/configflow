package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"configflow/internal/jsonx"
)

// Port of backend/common/dns_watchdog.py (module C25): local DNS liveness
// replies in delivered configurations, never profile drafts.
//
// The Go Agent checks this exact first rule before probing a DNS listener
// (backend/agents/go-agent/routes/dns_watchdog.go). Keep the rule in sync
// with it. All other names retain the original execution path; the probe
// bypasses caches, recorders and upstreams.

// WatchdogName is WATCHDOG_NAME.
const WatchdogName = "configflow-watchdog.invalid"

// WatchdogRule is WATCHDOG_RULE (a fresh copy):
// {"matches": ["qname full:configflow-watchdog.invalid"], "exec": "reject 3"}.
func WatchdogRule() *jsonx.Object {
	return jsonx.Obj("matches", []any{"qname full:" + WatchdogName}, "exec", "reject 3")
}

// watchdogRule is the shared constant for comparisons (never mutate it).
var watchdogRule = WatchdogRule()

// EnsureLocalDNSWatchdog is ensure_local_dns_watchdog(document): a copy of
// document with an idempotent local reply at each actual DNS server entry.
//
// Includes are left alone (the Agent does not enable its watchdog for such
// configurations). Non-sequence executors are wrapped without changing the
// original plugin; wrappers precede their server. Errors are Python's
// ValueError (*convert.PyError with Class "ValueError").
func EnsureLocalDNSWatchdog(document *jsonx.Object) (result *jsonx.Object, err error) {
	defer catch(&err)
	return ensureLocalDNSWatchdog(document), nil
}

func ensureLocalDNSWatchdog(document any) *jsonx.Object {
	result, _ := copyValue(document).(*jsonx.Object)
	if result == nil {
		raise(attributeError(document, "get"))
	}
	if truthy(result.Value("include")) {
		return result
	}
	value, present := result.Get("plugins")
	if !present {
		value = []any{}
	}
	plugins, ok := value.([]any)
	if !ok {
		raise(pyErr("ValueError", "DNS watchdog requires a plugins list"))
	}
	tagged := map[string]*jsonx.Object{}
	for _, element := range plugins {
		plugin, ok := element.(*jsonx.Object)
		if !ok || plugin == nil {
			raise(pyErr("ValueError", "DNS watchdog requires plugin objects"))
		}
		tag := plugin.Value("tag")
		if !truthy(tag) {
			continue // MosDNS server plugins need not have a tag.
		}
		text, isText := tag.(string)
		if !isText || tagged[text] != nil {
			raise(pyErr("ValueError", "DNS watchdog cannot use ambiguous plugin tags"))
		}
		tagged[text] = plugin
	}

	output := []any{}
	wrappers := map[string]string{}
	protected := map[string]bool{}
	protect := func(owner *jsonx.Object, key string) {
		entry, isText := owner.Value(key).(string)
		if isText && strings.Contains(entry, "${") {
			// Native OxiDNS resolves these in the core's environment; the
			// Agent pauses probing an entry it cannot verify locally.
			return
		}
		if !isText || tagged[entry] == nil {
			raise(pyErr("ValueError", "DNS watchdog cannot resolve the DNS server entry"))
		}
		target := tagged[entry]
		if target.Value("type") == "sequence" {
			if !protected[entry] {
				args, ok := target.Value("args").([]any)
				if !ok {
					raise(pyErr("ValueError", "DNS watchdog requires sequence rules to be a list"))
				}
				// Move an existing exact rule to the front, keeping the
				// other rules and their order.
				rules := []any{WatchdogRule()}
				for _, rule := range args {
					if !jsonx.Equal(rule, watchdogRule) {
						rules = append(rules, rule)
					}
				}
				target.Set("args", rules)
				protected[entry] = true
			}
			return
		}
		if _, done := wrappers[entry]; !done {
			sum := sha256.Sum256([]byte(entry))
			base := "cf_watchdog_" + hex.EncodeToString(sum[:])[:16]
			name, suffix := base, 1
			for tagged[name] != nil {
				name = base + "_" + itoa(suffix)
				suffix++
			}
			wrapper := jsonx.Obj("tag", name, "type", "sequence", "args", []any{
				WatchdogRule(), jsonx.Obj("exec", "$"+entry)})
			tagged[name] = wrapper
			wrappers[entry] = name
			output = append(output, wrapper)
		}
		owner.Set(key, wrappers[entry])
	}

	for _, element := range plugins {
		plugin := element.(*jsonx.Object)
		kind := plugin.Value("type")
		if strIn(kind, "udp_server", "tcp_server", "quic_server", "http_server") {
			args, ok := plugin.Value("args").(*jsonx.Object)
			if !ok || args == nil {
				raise(pyErr("ValueError", "DNS watchdog requires DNS server arguments"))
			}
			if kind == "http_server" {
				value, present := args.Get("entries")
				if !present {
					value = []any{}
				}
				entries, ok := value.([]any)
				if ok {
					for _, entry := range entries {
						if object, isObject := entry.(*jsonx.Object); !isObject || object == nil {
							ok = false
							break
						}
					}
				}
				if !ok {
					raise(pyErr("ValueError", "DNS watchdog requires HTTP DNS entries"))
				}
				for _, entry := range entries {
					protect(entry.(*jsonx.Object), "exec")
				}
			} else {
				protect(args, "entry")
			}
		}
		output = append(output, plugin)
	}
	if present {
		result.Set("plugins", output)
	}
	return result
}
