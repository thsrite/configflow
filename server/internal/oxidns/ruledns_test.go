package oxidns

import (
	"bytes"
	"compress/gzip"
	"configflow/internal/model"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

type ruleDNSReference struct {
	row
	Usages json.RawMessage `json:"usages"`
}

// TestRuleDNSPublicBoundary compares real-Python read/write entry points and the
// final generated configurations. No accepted-difference waiver is used.
func TestRuleDNSPublicBoundary(t *testing.T) {
	parallelBoundary(t)
	file, err := os.Open(testdataDir + "c27_public.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	decoder := json.NewDecoder(reader)
	count := 0
	var generated atomic.Int32
	for {
		var reference ruleDNSReference
		if err := decoder.Decode(&reference); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(reference.Fn+"/"+reference.Test, func(t *testing.T) {
			t.Parallel()
			args := reference.args(t)
			original := make([][]byte, len(args))
			for i, arg := range args {
				original[i] = ToJSON(arg)
			}
			got, err := callRuleDNSReference(t, reference.Fn, args)
			if reference.Fn != "apply_rule_dns_boundary" && reference.Fn != "hydrate_ruleset" {
				for i, arg := range args {
					if !bytes.Equal(original[i], ToJSON(arg)) {
						t.Fatalf("read changed argument %d, including stored YAML text", i)
					}
				}
			}
			if difference := compare(t, reference.row, got, err); difference != "" {
				t.Fatal(difference)
			}
			if reference.Err != nil && err != nil && err.Error() != reference.Err[1] {
				t.Fatalf("rejection message go=%q python=%q", err.Error(), reference.Err[1])
			}
			if len(reference.Usages) != 0 {
				var locked *RoutingLockedError
				if !errors.As(err, &locked) || !errors.Is(err, ErrRoutingLocked) {
					t.Fatalf("expected typed routing conflict: %v", err)
				}
				want, _ := FromJSON(reference.Usages)
				if difference := firstDifference(toNode(locked.Usages), want, "usages"); difference != "" {
					t.Fatal(difference)
				}
			}
			if difference := compareAfter(reference.row, args, got); difference != "" {
				t.Fatal(difference)
			}
			if reference.Fn == "apply_rule_dns_boundary" && err == nil && !isTrue(get(got, "generation_rejected")) {
				generated.Add(1)
			}
		})
	}
	// Cleanup runs after all reference subtests have joined.
	t.Cleanup(func() {
		if count < 282 || generated.Load() < 148 {
			t.Fatalf("incomplete public boundary: %d rows, %d generated outputs", count, generated.Load())
		}
		t.Logf("%d public references; %d final generated OxiDNS configurations compared", count, generated.Load())
	})
}

func callRuleDNSReference(t *testing.T, operation string, args []*Node) (*Node, error) {
	t.Helper()
	switch operation {
	case "rule_dns_state":
		options := RuleDNSReadOptions{}
		if len(args) > 2 && !isNone(args[2]) {
			options.Agents = []model.Agent{}
			for _, row := range items(args[2]) {
				var agent model.Agent
				if err := json.Unmarshal(ToJSON(row), &agent); err != nil {
					t.Fatal(err)
				}
				options.Agents = append(options.Agents, agent)
			}
		}
		state, err := ReadRuleDNS(profileOf(args[0]), options)
		return toNode(state), err
	case "rule_dns_entries":
		entries, err := RuleDNSEntries(profileOf(args[0]), ruleDNSIDs(args[1]), RuleDNSReadOptions{})
		return toNode(entries), err
	case "profile_agents":
		out := L()
		for _, agent := range ProfileAgents(profileOf(args[0])) {
			row, err := FromJSON(mustMarshal(t, agent))
			if err != nil {
				t.Fatal(err)
			}
			out.Content = append(out.Content, row)
		}
		return out, nil
	case "explicit_route":
		route, err := ExplicitRuleDNS(profileOf(args[0]), str(args[1]))
		if route == "" {
			return Null(), err
		}
		return S(route), err
	case "upstream_catalog":
		var bindings []DNSBinding
		if err := json.Unmarshal(ToJSON(args[2]), &bindings); err != nil {
			t.Fatal(err)
		}
		document := args[1]
		if isNone(document) {
			document = nil
		}
		catalog, err := UpstreamCatalog(args[0], document, bindings)
		return toNode(catalog), err
	case "hydrate_ruleset":
		before := ToJSON(args[0])
		result := HydrateRuleset(args[0], args[1], str(args[2]))
		if !bytes.Equal(before, ToJSON(args[0])) {
			t.Fatal("hydration changed caller-owned reference")
		}
		args[0] = result // C32 assigns the returned reference; Python edits its argument.
		return result, nil
	case "check_choice":
		return args[0], CheckDNSChoice(str(args[0]))
	case "invalidate_follow":
		return InvalidateFollow(profileOf(args[0]), args[1])
	case "apply_rule_dns_boundary":
		beforeBytes, afterBytes := ToJSON(args[0]), ToJSON(args[1])
		assignments := []DNSAssignment{}
		pairs(args[2], func(id string, choice *Node) bool {
			assignments = append(assignments, DNSAssignment{id, str(choice)})
			return true
		})
		result, err := ApplyRuleDNS(profileOf(args[0]), profileOf(args[1]), assignments,
			RuleDNSApplyOptions{BaseURL: "https://config.example", Lenient: !isTrue(args[3])})
		if !bytes.Equal(beforeBytes, ToJSON(args[0])) || !bytes.Equal(afterBytes, ToJSON(args[1])) {
			t.Fatal("profile input changed before committing replacement settings")
		}
		if err != nil {
			return nil, err
		}
		if strings.Contains(str(at(args[1], "oxidns", "native_config")), "# retained C27 draft") &&
			!strings.Contains(str(get(result.Settings, "native_config")), "# retained C27 draft") {
			t.Fatal("native rule write lost the caller's draft comment")
		}
		if result.Changed {
			set(args[1], "oxidns", result.Settings)
		}
		state, err := ReadRuleDNS(profileOf(args[1]), RuleDNSReadOptions{})
		if err != nil {
			return nil, err
		}
		text, generationErr := Generate(profileOf(args[1]), "https://config.example")
		var generated *Node
		if generationErr == nil {
			generated, err = LoadNativeYAML(text, NativeLabel)
			if err != nil {
				t.Fatalf("generated YAML cannot be read: %v", err)
			}
		}
		return M("warnings", toNode(result.Warnings), "settings", get(args[1], "oxidns"), "state", toNode(state),
			"generated", generated, "generation_rejected", generationErr != nil), nil
	}
	t.Fatalf("unmapped public entry: %s", operation)
	return nil, nil
}

func TestRuleDNSAssignmentOrder(t *testing.T) {
	p, err := ProfileFromJSON([]byte(`{"rule_configs":[{"id":"a","itemType":"rule","rule_type":"DOMAIN","value":"a.example"},{"id":"b","itemType":"rule","rule_type":"DOMAIN","value":"b.example"}],"oxidns":{"local_dns":"udp://192.0.2.53","remote_dns":"udp://192.0.2.54","future":{"retained":false}}}`))
	if err != nil {
		t.Fatal(err)
	}
	before := profileSnapshot(t, p)
	result, err := ApplyRuleDNS(p, p, []DNSAssignment{{"a", "drop"}, {"b", "proxy"}, {"a", "direct"}}, RuleDNSApplyOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !same(get(result.Settings, "direct_rules"), L("a")) || !same(get(result.Settings, "proxy_rules"), L("b")) ||
		!same(get(result.Settings, "future"), M("retained", false)) || !result.Changed {
		t.Fatalf("bulk assignment lost final choice or unknown settings: %s", ToJSON(result.Settings))
	}
	if !bytes.Equal(before, profileSnapshot(t, p)) {
		t.Fatal("bulk assignment changed its profile input")
	}
}

// profileSnapshot encodes everything a profile projection holds, so a test can
// prove an operation left its input unchanged.
func profileSnapshot(t *testing.T, p *Profile) []byte {
	t.Helper()
	source, err := model.Marshal(p.source)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Join([][]byte{ToJSON(p.settings), p.notes, source}, []byte{0})
}

func mustMarshal(t *testing.T, value any) []byte {
	t.Helper()
	data, err := model.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
