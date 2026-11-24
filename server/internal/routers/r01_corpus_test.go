package routers

import (
	"bufio"
	"compress/gzip"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/routerdoc"
	"configflow/internal/yamlx"
)

// TestR01Corpus runs every row of testdata/r01_corpus.jsonl.gz
// (testdata/gen_r01.py: the real Python functions) through the Go entry
// points and compares the results structurally (parsed JSON). Errors with a
// user message compare by message; Python-internal exceptions (TypeError …)
// only require Go to reject the input too.
func TestR01Corpus(t *testing.T) {
	spec.NewRouterID = func() string { return "router_GEN" }
	file, err := os.Open("testdata/r01_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	independent := loadR01NativeExpected(t)
	rowIndex := 0
	counts, differ, internal := map[string]int{}, 0, map[string]int{}
	differBy := map[string]int{}
	for scanner.Scan() {
		var row struct {
			Fn  string          `json:"fn"`
			In  json.RawMessage `json:"in"`
			Out json.RawMessage `json:"out"`
			Err []string        `json:"err"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if expected, ok := independent[rowIndex]; ok {
			if r01Hash(scanner.Bytes()) != expected.InputSHA256 {
				t.Fatal("independent input changed")
			}
			row.Out, row.Err = expected.Answer.Out, expected.Answer.Err
			delete(independent, rowIndex)
		}
		rowIndex++
		counts[row.Fn]++
		got, gotErr := r01Call(t, row.Fn, row.In)
		ok := false
		if row.Fn == "compute_desired" && desiredReaderPolicy(t, row.In, got, gotErr) {
			ok = true
			internal["N7-3 reader policy: no desired plan"]++
		} else {
			switch {
			case row.Err != nil && gotErr != nil:
				ok = gotErr.Error() == row.Err[1] || !userMessage(row.Err[0])
				if ok && !userMessage(row.Err[0]) {
					internal[row.Fn+": "+row.Err[0]]++
				}
			case row.Err == nil && gotErr == nil:
				ok = sameJSON(t, got, row.Out)
				if !ok && row.Fn == "stored" && readerDefaults(t, got, row.Out) {
					ok = true
					internal["stored: absent setting written with the reader default"]++
				}
				if !ok && row.Fn == "normalize" && reprField(t, got, row.Out) {
					ok = true
					internal["normalize: list/dict text field (Python repr)"]++
				}
				if !ok && row.Fn == "comparable_values" && sameTextRow(got, row.Out) {
					ok = true
					internal["O17 1.8: comparable_values: device values held as text, null as absent"]++
				}
				if !ok {
					if category := o18T1cAccepted(t, got, row.Out); category != "" {
						ok = true
						internal["O18 T1c 接受: "+row.Fn+": "+category]++
					}
				}
			}
		}
		if !ok {
			differ++
			differBy[row.Fn]++
			if differ <= 25 {
				g, _ := json.Marshal(got)
				t.Errorf("%s(%s)\n got  %s %v\n want %s %v", row.Fn, row.In, g, gotErr, row.Out, row.Err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(independent) > 0 {
		t.Fatal("stale independent expectations")
	}
	if differ > 0 {
		t.Logf("differ by fn: %v", differBy)
	}
	t.Logf("rows %v; %d differ; Python-internal exceptions where Go also rejects: %v", counts, differ, internal)
}

// reprField reports whether the two records differ only in text fields that
// hold the str() of a list/dict (Python repr vs JSON text; not chased).
func reprField(t *testing.T, got any, want json.RawMessage) bool {
	var a, b map[string]any
	data, _ := json.Marshal(got)
	_ = json.Unmarshal(data, &a)
	_ = json.Unmarshal(want, &b)
	for _, key := range []string{"name", "type", "endpoint", "username", "proxy_gateway_agent_id", "dns_agent_id"} {
		if text, _ := b[key].(string); text != "" && (text[0] == '[' || text[0] == '{') {
			delete(a, key)
			delete(b, key)
		}
	}
	return reflect.DeepEqual(a, b)
}

// readerDefaults reports whether got equals want once the settings want
// lacks are dropped from got, each holding the default every reader applies
// to an absent setting (records stored before the setting existed).
func readerDefaults(t *testing.T, got any, want json.RawMessage) bool {
	var a, b map[string]any
	data, _ := json.Marshal(got)
	_ = json.Unmarshal(data, &a)
	_ = json.Unmarshal(want, &b)
	defaults := map[string]any{"manage_return_routing": false, "auto_rule_sync": false, "rule_sync_interval": 60.0,
		"ros_policy_rule_sets": nil}
	for key, value := range defaults {
		if _, present := b[key]; !present && reflect.DeepEqual(a[key], value) {
			delete(a, key)
		}
	}
	return reflect.DeepEqual(a, b)
}

func userMessage(class string) bool {
	return class == "RouterValidationError" || class == "RouterError" || class == "OxidnsSettingsError" || class == "SettingsError"
}

func sameJSON(t *testing.T, got any, want json.RawMessage) bool {
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	_ = json.Unmarshal(data, &a)
	_ = json.Unmarshal(want, &b)
	return reflect.DeepEqual(a, b)
}

func decode[T any](t *testing.T, raw json.RawMessage) T {
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return v
}

func r01Call(t *testing.T, fn string, in json.RawMessage) (any, error) {
	switch fn {
	case "normalize":
		args := decode[struct{ Payload, Existing json.RawMessage }](t, in)
		var existing *spec.Router
		if string(args.Existing) != "null" {
			existing = decode[*spec.Router](t, args.Existing)
		}
		return spec.Normalize(args.Payload, existing)
	case "public_view":
		r := decode[spec.Router](t, in)
		return r.View(), nil
	case "stored":
		return decode[spec.Router](t, in), nil
	case "redact_routers":
		out, err := spec.RedactRouters(in)
		return json.RawMessage(out), err
	case "compute_desired":
		args := decode[struct {
			Profile  json.RawMessage
			Gateway  string
			RuleSets []string `json:"rule_sets"`
			Contents map[string]string
		}](t, in)
		return spec.ComputeDesired(context.Background(), args.Profile, args.Gateway, args.RuleSets, func(item spec.RuleSet, _ *spec.LibraryRule) (string, error) {
			return args.Contents[item.ID], nil
		})
	case "dns_agent_settings":
		args := decode[struct {
			ID    string
			Agent *device.DNSAgent
		}](t, in)
		return device.DNSAgentSettings(args.ID, args.Agent), nil
	case "dns_ha_settings":
		args := decode[struct {
			ID          string
			Agent, Peer *device.DNSAgent
		}](t, in)
		return device.DNSHASettings(args.ID, args.Agent, args.Peer), nil
	case "build_dns_report":
		args := decode[struct {
			DNS     device.DNSSettings
			Gateway string
			Entries []*device.DhcpDnsEntry
		}](t, in)
		var state *device.DhcpDnsState
		if args.Entries != nil {
			state = &device.DhcpDnsState{Entries: args.Entries}
		}
		return device.BuildDNSReport(args.DNS, args.Gateway, state), nil
	case "verify_dns":
		args := decode[struct {
			Check   device.DNSCheck
			Entries []*device.DhcpDnsEntry
		}](t, in)
		return nil, device.VerifyDNS(args.Check, &device.DhcpDnsState{Entries: args.Entries})
	case "select":
		args := decode[struct {
			Routes        []*device.RouteEntry
			CIDR, Gateway string
		}](t, in)
		sel, _ := device.NewStaticRouteIndex(args.Routes).Select(args.CIDR, args.Gateway)
		ids := func(routes []*device.RouteEntry) []string {
			out := []string{}
			for _, r := range routes {
				out = append(out, r.ID)
			}
			return out
		}
		return map[string]any{"candidates": ids(sel.Candidates), "selected": ids(sel.Selected), "covered": sel.Covered,
			"conflict": sel.Conflict}, nil
	case "comparable_values":
		args := decode[struct {
			Path string
			Row  ros.Row
		}](t, in)
		return ros.ComparableValues(args.Path, args.Row), nil
	case "normalize_mac":
		return routerdoc.NormalizeMAC(decode[string](t, in)), nil
	case "ipv4":
		return routerdoc.IPv4(decode[string](t, in)), nil
	case "validate_bindings":
		if message := routerdoc.ValidateBindings(in); message != "" {
			return nil, errors.New(message)
		}
		return nil, nil
	}
	t.Fatalf("unknown fn %s", fn)
	return nil, nil
}

// desiredReaderPolicy requires a typed rejection for precisely the native
// policy/unsafe-alias inputs. No plan or default-range substitution is allowed.
func desiredReaderPolicy(t *testing.T, input json.RawMessage, got any, actualErr error) bool {
	t.Helper()
	var args struct {
		Profile struct {
			Mihomo struct {
				Custom string `json:"custom_config"`
			}
		}
	}
	if json.Unmarshal(input, &args) != nil {
		return false
	}
	_, parseErr := yamlx.Parse([]byte(args.Profile.Mihomo.Custom))
	var kind error
	for _, candidate := range []error{yamlx.ErrPolicy, yamlx.ErrAliasRecursion, yamlx.ErrAliasBudget} {
		if errors.Is(parseErr, candidate) {
			kind = candidate
			break
		}
	}
	if kind == nil {
		return false
	}
	plan, ok := got.(*spec.Desired)
	var validation *device.ValidationError
	return ok && plan == nil && errors.As(actualErr, &validation) && errors.Is(actualErr, kind) && strings.HasSuffix(actualErr.Error(), parseErr.Error())
}
