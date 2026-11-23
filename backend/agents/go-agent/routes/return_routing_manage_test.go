package routes

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type fakeHost struct {
	noNeigh                     bool
	commands, inputs, mutations []string
	routes, rules, nft, vrfs    []map[string]any
	readErrors                  map[string]error
	readReplies                 map[string]string
	failNft                     bool
	// nft106 lists tables and chains without their comments, like nft 1.0.6
	// (Debian 12) does in "nft -j list ruleset".
	nft106                  bool
	loseRouteAddReply       bool
	ignoreDelete            string
	dropResourceAfterNFTAdd string
	nextHandle              int
}

func jsonText(value any) string { data, _ := json.Marshal(value); return string(data) }
func jsonMaps(value []map[string]any) []map[string]any {
	if value == nil {
		return []map[string]any{}
	}
	var copy []map[string]any
	_ = json.Unmarshal([]byte(jsonText(value)), &copy)
	return copy
}
func sameJSON(a, b any) bool  { return jsonText(a) == jsonText(b) }
func number(value any) string { return fmt.Sprint(value) }
func argValue(args []string, key string) string {
	for i := range args {
		if args[i] == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func argNumber(args []string, key string) int {
	value, _ := strconv.Atoi(argValue(args, key))
	return value
}

func installFakeHost(t *testing.T, host *fakeHost) {
	t.Helper()
	oldRun, oldInput, oldSysctl := runCommand, runCommandInput, sysctlRoot
	t.Cleanup(func() { runCommand, runCommandInput, sysctlRoot = oldRun, oldInput, oldSysctl })
	sysctlRoot = t.TempDir() // never touch the test host's /proc/sys
	runCommand = func(name string, args ...string) (string, error) {
		line := strings.Join(append([]string{name}, args...), " ")
		host.commands = append(host.commands, line)
		if err := host.readErrors[line]; err != nil {
			return "", err
		}
		if reply, ok := host.readReplies[line]; ok {
			return reply, nil
		}
		switch line {
		case "nft --version", "ip -V":
			return "ok", nil
		case "ip -4 route show default":
			return "default via 192.168.1.1 dev eth0 \n", nil
		case "ip -4 -o addr show dev eth0":
			return "2: eth0 inet 192.168.1.4/24 brd 192.168.1.255 scope global eth0\n", nil
		case "ip neigh show 192.168.1.1 dev eth0":
			if host.noNeigh {
				return "", nil
			}
			return "192.168.1.1 lladdr 00:E0:B4:66:D5:F9 REACHABLE\n", nil
		case "ping -c 1 -W 1 192.168.1.1":
			return "", nil
		case "ip -j -N -4 route show table all":
			return jsonText(jsonMaps(host.routes)), nil
		case "ip -j -N -4 rule show":
			return jsonText(jsonMaps(host.rules)), nil
		case "ip -j -d link show":
			return jsonText(jsonMaps(host.vrfs)), nil
		case "nft -j list ruleset":
			listed := jsonMaps(host.nft)
			if host.nft106 {
				for _, entry := range listed {
					for _, kind := range []string{"table", "chain"} {
						if body, ok := entry[kind].(map[string]any); ok {
							delete(body, "comment")
						}
					}
				}
			}
			return jsonText(map[string]any{"nftables": listed}), nil
		case "nft list tables":
			var lines []string
			for _, entry := range host.nft {
				if table, ok := entry["table"].(map[string]any); ok {
					lines = append(lines, "table "+number(table["family"])+" "+number(table["name"]))
				}
			}
			return strings.Join(lines, "\n"), nil
		case "ip -N -4 rule show":
			return "", nil
		}
		if name == "ip" && len(args) >= 4 && args[0] == "-4" && (args[1] == "route" || args[1] == "rule") && (args[2] == "add" || args[2] == "del") {
			host.mutations = append(host.mutations, line)
			kind, action := args[1], args[2]
			var resource map[string]any
			var list *[]map[string]any
			if kind == "route" {
				resource = map[string]any{"dst": args[3], "gateway": argValue(args, "via"), "dev": argValue(args, "dev"), "table": argNumber(args, "table"), "protocol": argNumber(args, "proto"), "metric": argNumber(args, "metric"), "flags": []any{}}
				if resource["gateway"] == "" || resource["dev"] == "" || resource["protocol"] != 242 || resource["metric"] == 0 {
					return "", fmt.Errorf("non-exact route mutation: %s", line)
				}
				list = &host.routes
			} else {
				mark := strings.Split(argValue(args, "fwmark"), "/")
				if len(mark) != 2 || argValue(args, "from") != "all" || argNumber(args, "protocol") != 242 {
					return "", fmt.Errorf("non-exact rule mutation: %s", line)
				}
				resource = map[string]any{"priority": argNumber(args, "priority"), "src": "all", "fwmark": mark[0], "fwmask": mark[1], "table": argNumber(args, "lookup"), "protocol": 242}
				list = &host.rules
			}
			index := -1
			for i, existing := range *list {
				if sameJSON(existing, resource) {
					index = i
					break
				}
			}
			if action == "add" {
				if index != -1 {
					return "", errors.New("File exists")
				}
				*list = append(*list, resource)
				if kind == "route" && host.loseRouteAddReply {
					host.loseRouteAddReply = false
					return "", errors.New("reply lost after kernel accepted route")
				}
			} else {
				if index == -1 {
					return "", errors.New("No such process")
				}
				if host.ignoreDelete == kind {
					return "", nil
				}
				*list = append((*list)[:index], (*list)[index+1:]...)
			}
			return "", nil
		}
		t.Errorf("unexpected host command: %s", line)
		return "", fmt.Errorf("unexpected host command: %s", line)
	}
	runCommandInput = func(input, name string, args ...string) (string, error) {
		host.inputs = append(host.inputs, input)
		line := name + " " + strings.Join(args, " ")
		host.mutations = append(host.mutations, line)
		if name != "nft" || !reflect.DeepEqual(args, []string{"-j", "-f", "-"}) {
			t.Errorf("unexpected nft command: %s", line)
			return "", fmt.Errorf("expected nft JSON transaction, got %s", line)
		}
		if host.failNft {
			return "", errors.New("nft transaction rejected")
		}
		return "", host.applyNFT(input)
	}
}

// Keep nft transactions atomic, including handles assigned by the kernel. The
// model rejects deleting a table or a nonempty chain, so broad cleanup cannot
// accidentally make a safety test pass.
func (host *fakeHost) applyNFT(input string) error {
	var document struct {
		NFTables []map[string]any `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(input), &document); err != nil {
		return err
	}
	if host.ignoreDelete == "nft" {
		for _, operation := range document.NFTables {
			if _, deleting := operation["delete"]; deleting {
				return nil
			}
		}
	}
	next := jsonMaps(host.nft)
	for _, operation := range document.NFTables {
		if _, ok := operation["metainfo"]; ok {
			continue
		}
		for action, raw := range operation {
			objects, ok := raw.(map[string]any)
			if !ok {
				return errors.New("bad nft operation")
			}
			for kind, rawObject := range objects {
				object, ok := rawObject.(map[string]any)
				if !ok {
					return errors.New("bad nft object")
				}
				if kind != "table" && kind != "chain" && kind != "rule" {
					return fmt.Errorf("unsupported nft object %s", kind)
				}
				if action == "delete" && kind == "table" {
					return errors.New("table deletion is forbidden")
				}
				index := -1
				for i, entry := range next {
					existing, ok := entry[kind].(map[string]any)
					if !ok || existing["family"] != object["family"] {
						continue
					}
					chainMatches := kind == "chain" && existing["table"] == object["table"] && (object["name"] != nil && existing["name"] == object["name"] || object["handle"] != nil && existing["handle"] == object["handle"])
					if kind == "table" && existing["name"] == object["name"] || chainMatches || kind == "rule" && existing["table"] == object["table"] && existing["chain"] == object["chain"] && object["handle"] != nil && existing["handle"] == object["handle"] {
						index = i
						break
					}
				}
				switch action {
				case "create", "add":
					if index >= 0 && kind != "rule" {
						return errors.New("File exists")
					}
					if kind != "table" {
						tablePresent, chainPresent := false, kind != "rule"
						for _, entry := range next {
							if table, ok := entry["table"].(map[string]any); ok && table["family"] == object["family"] && table["name"] == object["table"] {
								tablePresent = true
							}
							if chain, ok := entry["chain"].(map[string]any); ok && chain["family"] == object["family"] && chain["table"] == object["table"] && chain["name"] == object["chain"] {
								chainPresent = true
							}
						}
						if !tablePresent || !chainPresent {
							return errors.New("missing parent nft object")
						}
					}
					host.nextHandle++
					object["handle"] = float64(host.nextHandle)
					next = append(next, map[string]any{kind: object})
				case "delete":
					if index < 0 {
						return errors.New("No such file or directory")
					}
					if kind == "chain" {
						chainName := next[index]["chain"].(map[string]any)["name"]
						for _, entry := range next {
							if rule, ok := entry["rule"].(map[string]any); ok && rule["family"] == object["family"] && rule["table"] == object["table"] && rule["chain"] == chainName {
								return errors.New("chain is not empty")
							}
						}
					}
					next = append(next[:index], next[index+1:]...)
				default:
					return fmt.Errorf("unsafe nft action %s", action)
				}
			}
		}
	}
	host.nft = next
	if host.dropResourceAfterNFTAdd != "" {
		for _, operation := range document.NFTables {
			if _, added := operation["add"]; !added {
				continue
			}
			rows := &host.routes
			if host.dropResourceAfterNFTAdd == "rule" {
				rows = &host.rules
			}
			for index, row := range *rows {
				if number(row["table"]) == "104" && number(row["protocol"]) == "242" {
					*rows = append((*rows)[:index], (*rows)[index+1:]...)
					host.dropResourceAfterNFTAdd = ""
					break
				}
			}
			break
		}
	}
	return nil
}

func testConfig(t *testing.T, mihomo string) *Config {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(mihomo), 0644); err != nil {
		t.Fatal(err)
	}
	return &Config{AgentConfigFile: filepath.Join(dir, "config-mihomo.json"), ConfigPath: configPath}
}

const tunConfig = "tun:\n  enable: true\n  device: Meta\n  auto-route: true\n  auto-redirect: true\n"

func sampleSpec() ReturnRoutingSpec {
	var spec ReturnRoutingSpec
	_ = json.Unmarshal([]byte(`{"router_ip":"192.168.1.1","proxy_dst":["198.18.0.0/16","91.108.0.0/16"],"mark":"0x400000","table":104,"priority":104,"safety_version":1}`), &spec)
	return spec
}
func assertNoWrites(t *testing.T, host *fakeHost) {
	t.Helper()
	if len(host.mutations) != 0 {
		t.Fatalf("refusal must not write to host: %v", host.mutations)
	}
}
func readStateJSON(t *testing.T, cfg *Config) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(cfg.AgentConfigFile), "return-routing.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err = json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestValidateSpec(t *testing.T) {
	cases := map[string]func(*ReturnRoutingSpec){
		"old_protocol": func(s *ReturnRoutingSpec) { s.SafetyVersion = 0 },
		"router":       func(s *ReturnRoutingSpec) { s.RouterIP = "router" },
		"mark":         func(s *ReturnRoutingSpec) { s.Mark = "0" },
		"table":        func(s *ReturnRoutingSpec) { s.Table = 254 },
		"dst":          func(s *ReturnRoutingSpec) { s.ProxyDst = []string{"2001:db8::/32"} },
		"empty":        func(s *ReturnRoutingSpec) { s.ProxyDst = nil },
		"iface":        func(s *ReturnRoutingSpec) { s.LanInterface = "eth0; reboot" },
		"legacy":       func(s *ReturnRoutingSpec) { s.AdoptLegacy = []string{"inet mihomo_return"} },
		"priority":     func(s *ReturnRoutingSpec) { s.Priority = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := sampleSpec()
			mutate(&spec)
			if _, err := validateSpec(spec); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if _, err := validateSpec(sampleSpec()); err != nil {
		t.Fatal(err)
	}
	byRouter := routerSpec()
	if _, err := validateSpec(byRouter); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ReturnRoutingSpec){
		"router_protocol_with_dst": func(s *ReturnRoutingSpec) { s.ProxyDst = []string{"198.18.0.0/16"} },
		"unknown_protocol":         func(s *ReturnRoutingSpec) { s.SafetyVersion = 3 },
	} {
		spec := routerSpec()
		mutate(&spec)
		if _, err := validateSpec(spec); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

// routerSpec is sampleSpec on protocol 2: no destination list.
func routerSpec() ReturnRoutingSpec {
	spec := sampleSpec()
	spec.SafetyVersion, spec.ProxyDst = returnProtocolByRouter, nil
	return spec
}

func TestReturnRoutingMovesBetweenDestinationAndRouterProtocols(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	initial := readStateJSON(t, cfg)["ownership"].(map[string]any)
	result, err := ApplyReturnRouting(cfg, routerSpec())
	if err != nil {
		t.Fatal(err)
	}
	if result["safety_version"] != returnProtocolByRouter {
		t.Fatalf("apply result = %v", result)
	}
	rules := jsonText(host.nft)
	if strings.Contains(rules, "daddr") || strings.Contains(rules, "198.18.0.0") || !strings.Contains(rules, `"nfproto"`) ||
		!strings.Contains(rules, "00:e0:b4:66:d5:f9") {
		t.Fatalf("protocol 2 must match the router, not destinations: %s", rules)
	}
	if len(host.routes) != 2 || len(host.rules) != 1 || len(host.nft) != 6 {
		t.Fatalf("switching protocols must not accumulate resources: %v %v %v", host.routes, host.rules, host.nft)
	}
	state := readStateJSON(t, cfg)
	owner := state["ownership"].(map[string]any)
	if owner["token"] != initial["token"] || state["active"] != true || owner["pending"] != false {
		t.Fatalf("ownership was lost switching protocols: %v", state)
	}
	if status := managedReturnStatus(cfg); status["safety_version"] != returnProtocolByRouter || status["table_present"] != true {
		t.Fatalf("status = %v", status)
	}
	// An Agent restart finds the protocol 2 resources current and leaves them alone.
	before := len(host.mutations)
	reapplyReturnRouting(cfg)
	if len(host.mutations) != before {
		t.Fatalf("replay rewrote current protocol 2 resources: %v", host.mutations[before:])
	}
	// A release rollback can go back to the destination list.
	if _, err = ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	if rules = jsonText(host.nft); !strings.Contains(rules, "198.18.0.0") || strings.Contains(rules, `"nfproto"`) {
		t.Fatalf("protocol 1 destinations not restored: %s", rules)
	}
}

func TestReturnSafetyStatusListsProtocols(t *testing.T) {
	installFakeHost(t, &fakeHost{})
	status := returnSafetyStatus(testConfig(t, tunConfig))
	if status["version"] != 1 || !sameJSON(status["protocols"], []int{1, 2}) {
		t.Fatalf("status = %v", status)
	}
}
func TestMihomoConflicts(t *testing.T) {
	cfg := testConfig(t, tunConfig+"routing-mark: 6666\n")
	routing := readMihomoRouting(cfg.ConfigPath)
	if !routing.tunEnabled || routing.tunDevice != "Meta" {
		t.Fatalf("routing = %+v", routing)
	}
	spec, _ := validateSpec(sampleSpec())
	if err := checkMihomoConflicts(spec, routing); err != nil {
		t.Fatal(err)
	}
	overlap := sampleSpec()
	overlap.Mark = "0x4"
	spec, _ = validateSpec(overlap)
	if err := checkMihomoConflicts(spec, routing); err == nil || !strings.Contains(err.Error(), "auto-redirect") {
		t.Fatalf("expected auto-redirect overlap, got %v", err)
	}
	spec, _ = validateSpec(sampleSpec())
	spec.Table = 2022
	if err := checkMihomoConflicts(spec, routing); err == nil {
		t.Fatal("expected table conflict")
	}
}

func TestReturnRoutingInvalidMihomoConfigRefusedWithoutWrites(t *testing.T) {
	cases := map[string]string{
		"missing":                "",
		"bad_yaml":               "tun: [\n",
		"mark_map":               "routing-mark: {value: 4194304}\n",
		"mark_overflow":          "routing-mark: 4294967296\n",
		"mark_float_overlap":     "routing-mark: 4194304.0\n",
		"mark_table_alias":       "routing-mark: main\n",
		"tun_mark_map":           "tun:\n  enable: true\n  auto-redirect-input-mark: {value: 4194304}\n",
		"tun_mark_overflow":      "tun:\n  enable: true\n  auto-redirect-output-mark: 4294967296\n",
		"tun_not_map":            "tun: unexpected\n",
		"tun_enable_not_boolean": "tun:\n  enable: yes\n",
	}
	for name, invalid := range cases {
		for _, previouslyManaged := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/existing=%v", name, previouslyManaged), func(t *testing.T) {
				host := &fakeHost{}
				installFakeHost(t, host)
				cfg := testConfig(t, tunConfig)
				if previouslyManaged {
					if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
						t.Fatal(err)
					}
				}
				if name == "missing" {
					if err := os.Remove(cfg.ConfigPath); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(cfg.ConfigPath, []byte(invalid), 0644); err != nil {
					t.Fatal(err)
				}
				before := jsonText([]any{host.routes, host.rules, host.nft})
				host.mutations = nil
				if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil || !strings.Contains(err.Error(), "mihomo") {
					t.Fatalf("invalid mihomo evidence must block application: %v", err)
				}
				status := returnSafetyStatus(cfg)
				if status["ok"] != false || len(status["blockers"].([]string)) == 0 {
					t.Fatalf("invalid mihomo evidence must block status: %v", status)
				}
				assertNoWrites(t, host)
				if before != jsonText([]any{host.routes, host.rules, host.nft}) {
					t.Fatal("configuration uncertainty must preserve existing resources")
				}
			})
		}
	}
}

func TestLegacyReturnRoutingRefusedWithoutWrites(t *testing.T) {
	for _, variant := range []string{"adopt_request", "old_state", "corrupt_state"} {
		t.Run(variant, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			spec := sampleSpec()
			script := filepath.Join(t.TempDir(), "mihomo-return.start")
			original := []byte("nft -f /etc/local.d/mihomo-return.nft\n")
			if err := os.WriteFile(script, original, 0755); err != nil {
				t.Fatal(err)
			}
			if variant == "adopt_request" {
				spec.AdoptLegacy = []string{"inet mihomo_return"}
			} else {
				data := []byte(`{"spec":` + jsonText(spec) + `,"applied_at":"2026-10-17T01:00:00Z"}`)
				if variant == "corrupt_state" {
					data = []byte(`{"spec":`)
				}
				if err := os.WriteFile(filepath.Join(filepath.Dir(cfg.AgentConfigFile), "return-routing.json"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := ApplyReturnRouting(cfg, spec); err == nil {
				t.Fatal("legacy or uncertain ownership must be refused")
			}
			assertNoWrites(t, host)
			if actual, err := os.ReadFile(script); err != nil || !bytes.Equal(actual, original) {
				t.Fatal("legacy startup script changed")
			}
		})
	}
}
func TestReturnRoutingOccupiedResourcesRefusedWithoutWrites(t *testing.T) {
	cases := map[string]*fakeHost{
		"route_table":   {routes: []map[string]any{{"dst": "default", "gateway": "192.168.1.9", "dev": "eth0", "table": 104, "protocol": 4}}},
		"rule_priority": {rules: []map[string]any{{"priority": 104, "src": "all", "table": 105}}},
		"rule_table":    {rules: []map[string]any{{"priority": 200, "src": "all", "table": 104}}},
		"mark_overlap":  {rules: []map[string]any{{"priority": 200, "src": "all", "fwmark": "0x400000", "fwmask": "0x400000", "table": 105}}},
		"nft_table":     {nft: []map[string]any{{"table": map[string]any{"family": "inet", "name": managedReturnTable, "handle": 1, "comment": "manual"}}}},
		"vrf_table":     {vrfs: []map[string]any{{"ifindex": 5, "ifname": "vrf-existing", "linkinfo": map[string]any{"info_kind": "vrf", "info_data": map[string]any{"table": 104}}}}},
	}
	for name, host := range cases {
		t.Run(name, func(t *testing.T) {
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			before := jsonText([]any{host.routes, host.rules, host.nft, host.vrfs})
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil {
				t.Fatal("occupied resource must be refused")
			}
			assertNoWrites(t, host)
			if after := jsonText([]any{host.routes, host.rules, host.nft, host.vrfs}); after != before {
				t.Fatal("foreign resources changed")
			}
		})
	}
}
func TestReturnRoutingUnreadableResourcesRefusedWithoutWrites(t *testing.T) {
	for _, command := range []string{"ip -j -N -4 route show table all", "ip -j -N -4 rule show", "nft -j list ruleset", "ip -j -d link show"} {
		for _, malformed := range []bool{false, true} {
			t.Run(command+fmt.Sprint(malformed), func(t *testing.T) {
				host := &fakeHost{}
				if malformed {
					host.readReplies = map[string]string{command: "not-json"}
				} else {
					host.readErrors = map[string]error{command: errors.New("permission denied")}
				}
				installFakeHost(t, host)
				if _, err := ApplyReturnRouting(testConfig(t, tunConfig), sampleSpec()); err == nil {
					t.Fatal("unreadable resource must not count as empty")
				}
				assertNoWrites(t, host)
			})
		}
	}
}
func TestReturnRoutingAddsAndPreciselyRemovesOwnedResources(t *testing.T) {
	foreignRoute := map[string]any{"dst": "10.0.0.0/8", "gateway": "192.168.1.9", "dev": "eth0", "table": 105, "protocol": 4, "metric": 10}
	foreignRule := map[string]any{"priority": 200, "src": "all", "table": 105}
	foreignTable := map[string]any{"table": map[string]any{"family": "inet", "name": "other_service", "handle": 100}}
	host := &fakeHost{routes: []map[string]any{foreignRoute}, rules: []map[string]any{foreignRule}, nft: []map[string]any{foreignTable}, nextHandle: 100}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	result, err := ApplyReturnRouting(cfg, sampleSpec())
	if err != nil {
		t.Fatal(err)
	}
	if result["success"] != true {
		t.Fatalf("apply = %v", result)
	}
	if len(host.routes) != 3 || len(host.rules) != 2 {
		t.Fatalf("expected two routes and one policy rule: routes=%v rules=%v", host.routes, host.rules)
	}
	state := readStateJSON(t, cfg)
	owner, ok := state["ownership"].(map[string]any)
	if !ok || owner["version"] != float64(1) || owner["token"] == "" || owner["token"] == nil {
		t.Fatalf("missing durable ownership: %v", state)
	}
	metric := host.routes[1]["metric"]
	if metric == 0 || !sameJSON(metric, host.routes[2]["metric"]) {
		t.Fatal("owned route metric must be stable and nonzero")
	}
	for _, route := range host.routes[1:] {
		if number(route["protocol"]) != "242" {
			t.Fatalf("route lacks protocol ownership: %v", route)
		}
	}
	// Chains carry the token in their names, rules in their comments; the
	// fixed-name table carries none (nft 1.0.6 does not list its comment).
	for _, entry := range host.nft {
		for kind, raw := range entry {
			object := raw.(map[string]any)
			if object["name"] == "other_service" || kind == "table" {
				continue
			}
			evidence := number(object["comment"])
			if kind == "chain" {
				evidence = number(object["name"])
			}
			if !strings.Contains(evidence, number(owner["token"])) {
				t.Fatalf("nft object lacks ownership token: %v", object)
			}
		}
	}
	encoded := strings.Join(host.inputs, "\n")
	for _, wanted := range []string{"00:e0:b4:66:d5:f9", "198.18.0.0", "91.108.0.0", "Meta"} {
		if !strings.Contains(encoded, wanted) {
			t.Errorf("published nft rules missing %q", wanted)
		}
	}
	result, err = RemoveReturnRouting(cfg, false)
	if err != nil || result["success"] != true {
		t.Fatalf("remove = %v, %v", result, err)
	}
	if !sameJSON(host.routes, []map[string]any{foreignRoute}) || !sameJSON(host.rules, []map[string]any{foreignRule}) {
		t.Fatalf("foreign routes/rules changed: %v %v", host.routes, host.rules)
	}
	if len(host.nft) != 2 {
		t.Fatalf("only foreign and empty owned tables should remain: %v", host.nft)
	}
	if !sameJSON(host.nft[0], foreignTable) {
		t.Fatal("foreign nft table changed")
	}
	ownedTable, ok := host.nft[1]["table"].(map[string]any)
	if !ok || ownedTable["name"] != managedReturnTable {
		t.Fatalf("owned table must be retained: %v", host.nft)
	}
	remaining := readStateJSON(t, cfg)
	if remaining["ownership"] == nil {
		t.Fatal("retained table must keep durable ownership")
	}
	if remaining["active"] != false {
		t.Fatal("removed return routing must not be active for boot replay")
	}
}

func TestReturnRoutingRepeatedApplyUpdatesScopeAndReusesEmptyTable(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
		t.Fatal(err)
	}
	initial := readStateJSON(t, cfg)["ownership"].(map[string]any)
	table := jsonText(host.nft[0])
	updated := sampleSpec()
	updated.ProxyDst = []string{"203.0.113.0/24"}
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := ApplyReturnRouting(cfg, updated); err != nil {
			t.Fatalf("scope update %d: %v", iteration, err)
		}
		if len(host.routes) != 2 || len(host.rules) != 1 || len(host.nft) != 6 {
			t.Fatalf("reapply must not accumulate resources: %v %v %v", host.routes, host.rules, host.nft)
		}
		rules := jsonText(host.nft)
		if !strings.Contains(rules, "203.0.113.0") || strings.Contains(rules, "198.18.0.0") || strings.Contains(rules, "91.108.0.0") {
			t.Fatalf("old destination scope remains: %s", rules)
		}
		state := readStateJSON(t, cfg)
		owner := state["ownership"].(map[string]any)
		if owner["token"] != initial["token"] || owner["metric"] != initial["metric"] || state["active"] != true || owner["pending"] != false {
			t.Fatalf("ownership was lost on reapply: %v", state)
		}
		if table != jsonText(host.nft[0]) {
			t.Fatal("reapply recreated the owned nft table")
		}
	}
	for iteration := 0; iteration < 2; iteration++ {
		if _, err := RemoveReturnRouting(cfg, false); err != nil {
			t.Fatalf("removal %d: %v", iteration, err)
		}
		if len(host.routes) != 0 || len(host.rules) != 0 || len(host.nft) != 1 || table != jsonText(host.nft[0]) {
			t.Fatal("removal must retain only the owned empty table")
		}
	}
	if _, err := ApplyReturnRouting(cfg, updated); err != nil {
		t.Fatalf("apply after removal: %v", err)
	}
	state := readStateJSON(t, cfg)
	owner := state["ownership"].(map[string]any)
	if len(host.routes) != 2 || len(host.rules) != 1 || len(host.nft) != 6 || table != jsonText(host.nft[0]) || owner["token"] != initial["token"] || state["active"] != true || owner["pending"] != false {
		t.Fatalf("cannot safely reuse retained ownership: %v", state)
	}
}

func TestReturnRoutingBootReplayRepairsMissingOwnedResources(t *testing.T) {
	for _, missing := range []string{"all", "route", "rule", "nft_rule"} {
		t.Run(missing, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
				t.Fatal(err)
			}
			initial := readStateJSON(t, cfg)["ownership"].(map[string]any)
			if status := managedReturnStatus(cfg); status["table_present"] != true {
				t.Fatalf("complete initial resources must be healthy: %v", status)
			}
			switch missing {
			case "all":
				host.routes, host.rules, host.nft = nil, nil, nil
			case "route":
				host.routes = host.routes[1:]
			case "rule":
				host.rules = nil
			case "nft_rule":
				for index, entry := range host.nft {
					if _, ok := entry["rule"]; ok {
						host.nft = append(host.nft[:index], host.nft[index+1:]...)
						break
					}
				}
			}
			host.mutations = nil
			if status := managedReturnStatus(cfg); status["table_present"] != false {
				t.Fatalf("missing %s must not appear synchronized: %v", missing, status)
			}
			if safety := returnSafetyStatus(cfg); safety["ok"] != true {
				t.Fatalf("missing owned resources should permit safe repair: %v", safety)
			}
			assertNoWrites(t, host)
			reapplyReturnRouting(cfg)
			state := readStateJSON(t, cfg)
			owner := state["ownership"].(map[string]any)
			if len(host.routes) != 2 || len(host.rules) != 1 || len(host.nft) != 6 || state["active"] != true || owner["pending"] != false {
				t.Fatalf("boot replay did not restore complete resources: %v", state)
			}
			if owner["token"] != initial["token"] || owner["metric"] != initial["metric"] {
				t.Fatal("boot replay replaced durable ownership identity")
			}
			if status := managedReturnStatus(cfg); status["table_present"] != true {
				t.Fatalf("repaired resources must pass full readback: %v", status)
			}
		})
	}
}

func TestReturnRoutingBootReplayPreservesUnsafeOrInactiveState(t *testing.T) {
	for _, variant := range []string{"legacy", "pending", "inactive", "bad_config", "changed_route", "foreign_route"} {
		t.Run(variant, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
				t.Fatal(err)
			}
			state, err := loadReturnState(cfg)
			if err != nil {
				t.Fatal(err)
			}
			switch variant {
			case "legacy":
				state.Ownership = nil
			case "pending":
				state.Ownership.Pending = true
			case "inactive":
				if _, err := RemoveReturnRouting(cfg, false); err != nil {
					t.Fatal(err)
				}
				state, err = loadReturnState(cfg)
				if err != nil {
					t.Fatal(err)
				}
			case "bad_config":
				if err := os.WriteFile(cfg.ConfigPath, []byte("tun: [\n"), 0644); err != nil {
					t.Fatal(err)
				}
			case "changed_route":
				host.routes[0]["gateway"] = "192.168.1.9"
			case "foreign_route":
				host.routes = append(host.routes, map[string]any{"dst": "203.0.113.0/24", "gateway": "192.168.1.9", "dev": "eth0", "table": 104, "protocol": 4})
			}
			if err := saveReturnState(cfg, state); err != nil {
				t.Fatal(err)
			}
			beforeState := jsonText(readStateJSON(t, cfg))
			beforeHost := jsonText([]any{host.routes, host.rules, host.nft})
			host.mutations = nil
			reapplyReturnRouting(cfg)
			assertNoWrites(t, host)
			if beforeState != jsonText(readStateJSON(t, cfg)) || beforeHost != jsonText([]any{host.routes, host.rules, host.nft}) {
				t.Fatal("refused boot replay changed resources or ownership")
			}
			if variant != "bad_config" {
				if status := managedReturnStatus(cfg); status["table_present"] != false {
					t.Fatalf("inactive or unproven resources must not appear healthy: %v", status)
				}
			}
		})
	}
}

func TestReturnRoutingFinalReadbackDetectsLostEarlierWrites(t *testing.T) {
	for _, lost := range []string{"route", "rule"} {
		t.Run(lost, func(t *testing.T) {
			foreignRoute := map[string]any{"dst": "10.0.0.0/8", "gateway": "192.168.1.9", "dev": "eth0", "table": 105, "protocol": 4, "metric": 10}
			foreignRule := map[string]any{"priority": 200, "src": "all", "table": 105}
			foreignTable := map[string]any{"table": map[string]any{"family": "inet", "name": "other_service", "handle": 100}}
			host := &fakeHost{routes: []map[string]any{foreignRoute}, rules: []map[string]any{foreignRule}, nft: []map[string]any{foreignTable}, nextHandle: 100, dropResourceAfterNFTAdd: lost}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			result, err := ApplyReturnRouting(cfg, sampleSpec())
			if err == nil || !strings.Contains(err.Error(), "最终回读不完整") || result["success"] == true {
				t.Fatalf("lost %s must fail final confirmation: result=%v error=%v", lost, result, err)
			}
			if host.dropResourceAfterNFTAdd != "" {
				t.Fatal("test did not remove an earlier write during the successful nft transaction")
			}
			if !sameJSON(host.routes, []map[string]any{foreignRoute}) || !sameJSON(host.rules, []map[string]any{foreignRule}) {
				t.Fatalf("cleanup changed foreign resources or left owned routes/rules: %v %v", host.routes, host.rules)
			}
			if len(host.nft) != 2 || !sameJSON(host.nft[0], foreignTable) {
				t.Fatalf("cleanup must preserve foreign nft config and the empty owned table: %v", host.nft)
			}
			if table, ok := host.nft[1]["table"].(map[string]any); !ok || table["name"] != managedReturnTable {
				t.Fatalf("unexpected managed leftovers: %v", host.nft[1])
			}
			state := readStateJSON(t, cfg)
			owner := state["ownership"].(map[string]any)
			if state["active"] != false || owner["pending"] != false || len(owner["routes"].([]any)) != 0 || len(owner["rules"].([]any)) != 0 {
				t.Fatalf("safe cleanup must finish with inactive ownership: %v", state)
			}
		})
	}
}

func TestReturnRoutingFailedNFTDoesNotFlushForeignResources(t *testing.T) {
	foreign := map[string]any{"dst": "10.0.0.0/8", "table": 105, "protocol": 4}
	host := &fakeHost{routes: []map[string]any{foreign}, failNft: true}
	installFakeHost(t, host)
	if _, err := ApplyReturnRouting(testConfig(t, tunConfig), sampleSpec()); err == nil {
		t.Fatal("expected rejected nft transaction")
	}
	if !sameJSON(host.routes, []map[string]any{foreign}) || len(host.rules) != 0 || len(host.nft) != 0 {
		t.Fatalf("partial apply did not clean up precisely: %+v", host)
	}
	for _, command := range host.commands {
		if strings.Contains(command, " flush ") || strings.Contains(command, " replace ") || strings.Contains(command, "nft delete table") {
			t.Fatalf("broad cleanup forbidden: %s", command)
		}
	}
}

func TestReturnRoutingChangedOwnershipRefusesRemovalWithoutWrites(t *testing.T) {
	for _, kind := range []string{"route", "rule", "nft_rule", "nft_handle", "foreign_chain_rule"} {
		t.Run(kind, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "route":
				host.routes[0]["gateway"] = "192.168.1.9"
			case "rule":
				host.rules[0]["src"] = "192.168.1.100"
			case "nft_rule", "nft_handle", "foreign_chain_rule":
				for _, entry := range host.nft {
					rule, ok := entry["rule"].(map[string]any)
					if !ok {
						continue
					}
					if kind == "nft_rule" {
						rule["expr"] = []any{map[string]any{"accept": nil}}
					}
					if kind == "nft_handle" {
						rule["handle"] = rule["handle"].(float64) + 1000
					}
					if kind == "foreign_chain_rule" {
						host.nft = append(host.nft, map[string]any{"rule": map[string]any{"family": "inet", "table": managedReturnTable, "chain": rule["chain"], "handle": 9999, "expr": []any{map[string]any{"accept": nil}}, "comment": "manual"}})
					}
					break
				}
			}
			before := jsonText([]any{host.routes, host.rules, host.nft})
			host.mutations = nil
			if _, err := RemoveReturnRouting(cfg, false); err == nil {
				t.Fatal("changed ownership must prevent deletion")
			}
			assertNoWrites(t, host)
			if before != jsonText([]any{host.routes, host.rules, host.nft}) {
				t.Fatal("unconfirmed resources were changed")
			}
		})
	}
}

func TestReturnRoutingLostWriteReplyRetainsPendingEvidence(t *testing.T) {
	foreign := map[string]any{"dst": "10.0.0.0/8", "table": 105, "protocol": 4}
	host := &fakeHost{routes: []map[string]any{foreign}, loseRouteAddReply: true}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil {
		t.Fatal("lost write reply must not be reported as success")
	}
	if len(host.routes) != 2 || !sameJSON(host.routes[0], foreign) {
		t.Fatalf("unconfirmed route and foreign resource must be preserved: %v", host.routes)
	}
	state := readStateJSON(t, cfg)
	owner, ok := state["ownership"].(map[string]any)
	if !ok || owner["pending"] != true {
		t.Fatalf("incomplete transaction must retain durable evidence: %v", state)
	}
	host.mutations = nil
	if _, err := ApplyReturnRouting(cfg, sampleSpec()); err == nil {
		t.Fatal("pending transaction must prevent blind retry")
	}
	assertNoWrites(t, host)
	if _, err := RemoveReturnRouting(cfg, false); err == nil {
		t.Fatal("pending transaction must prevent unconfirmed deletion")
	}
	assertNoWrites(t, host)
}

func TestReturnRoutingSuccessfulDeleteWithoutEffectPreservesJournal(t *testing.T) {
	for _, kind := range []string{"route", "rule", "nft"} {
		t.Run(kind, func(t *testing.T) {
			host := &fakeHost{}
			installFakeHost(t, host)
			cfg := testConfig(t, tunConfig)
			if _, err := ApplyReturnRouting(cfg, sampleSpec()); err != nil {
				t.Fatal(err)
			}
			before := readStateJSON(t, cfg)["ownership"].(map[string]any)
			host.ignoreDelete = kind
			if _, err := RemoveReturnRouting(cfg, false); err == nil {
				t.Fatal("successful command without deletion must fail readback")
			}
			after := readStateJSON(t, cfg)["ownership"].(map[string]any)
			if after["pending"] != true {
				t.Fatalf("unconfirmed removal must remain pending: %v", after)
			}
			key := kind + "s"
			if kind == "nft" {
				key = "nft"
			}
			if !sameJSON(before[key], after[key]) {
				t.Fatalf("unremoved %s ownership was discarded: before=%v after=%v", kind, before[key], after[key])
			}
			// The journal still proves every remaining object, so a retry
			// deletes only recorded resources and keeps the evidence on failure.
			if _, err := RemoveReturnRouting(cfg, false); err == nil {
				t.Fatal("retry without effect must fail readback again")
			}
			again := readStateJSON(t, cfg)["ownership"].(map[string]any)
			if again["pending"] != true || !sameJSON(after[key], again[key]) {
				t.Fatalf("failed retry discarded ownership: %v", again)
			}
			host.ignoreDelete = ""
			if _, err := RemoveReturnRouting(cfg, false); err != nil {
				t.Fatalf("retry of a recorded removal must finish: %v", err)
			}
			if len(host.routes) != 0 || len(host.rules) != 0 || len(host.nft) != 1 {
				t.Fatalf("recovered removal left resources: %v %v %v", host.routes, host.rules, host.nft)
			}
		})
	}
}

func TestReturnRoutingHandlerPutAndDelete(t *testing.T) {
	host := &fakeHost{}
	installFakeHost(t, host)
	cfg := testConfig(t, tunConfig)
	body, _ := json.Marshal(sampleSpec())
	recorder := httptest.NewRecorder()
	ReturnRoutingHandler(cfg)(recorder, httptest.NewRequest(http.MethodPut, "/api/return-routing", bytes.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("put code = %d body = %s", recorder.Code, recorder.Body.String())
	}
	recorder = httptest.NewRecorder()
	ReturnRoutingHandler(cfg)(recorder, httptest.NewRequest(http.MethodPut, "/api/return-routing", strings.NewReader(`{"bogus":1}`)))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unknown fields must be rejected, code = %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	ReturnRoutingHandler(cfg)(recorder, httptest.NewRequest(http.MethodDelete, "/api/return-routing", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"removed":true`) {
		t.Fatalf("delete code = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
func TestApplyRequiresRouterMAC(t *testing.T) {
	host := &fakeHost{noNeigh: true}
	installFakeHost(t, host)
	if _, err := ApplyReturnRouting(testConfig(t, tunConfig), sampleSpec()); err == nil || !strings.Contains(err.Error(), "MAC") {
		t.Fatalf("expected MAC resolution error, got %v", err)
	}
	if !strings.Contains(strings.Join(host.commands, "\n"), "ping -c 1 -W 1 192.168.1.1") {
		t.Error("router MAC lookup should populate the neighbor table once")
	}
	assertNoWrites(t, host)
}
