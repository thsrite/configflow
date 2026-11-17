package oxidns

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
)

func TestC27ReviewHelperOwnership(t *testing.T) {
	features := M("cache_enabled", true, "cache_lazy_ttl", 300)
	action := M("action", "upstream", "source", "local", "upstream", M("addr", "udp://192.0.2.53"))
	before := ToJSON(features)
	plugins, err := HelperPlugins([]string{RouteKey(action)}, M("rule", action), features)
	if err != nil {
		t.Fatal(err)
	}
	for _, plugin := range plugins {
		if isOneOf(get(plugin, "type"), "cache") {
			at(plugin, "args", "lazy_cache_ttl").Value = "0"
		}
	}
	if !bytes.Equal(before, ToJSON(features)) {
		t.Fatal("editing a returned helper changed caller-owned settings")
	}
}

type c27ReviewReference struct {
	ruleDNSReference
	D2 *struct {
		Stage     string          `json:"stage"`
		Source    string          `json:"source"`
		LegacyKey string          `json:"legacy_key"`
		NativeKey string          `json:"native_key"`
		Delivered json.RawMessage `json:"delivered"`
	} `json:"d2"`
}

func c27ReviewReferences(t *testing.T) []c27ReviewReference {
	t.Helper()
	file, err := os.Open(testdataDir + "review_c27.jsonl.gz")
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
	var rows []c27ReviewReference
	for {
		var reference c27ReviewReference
		if err := decoder.Decode(&reference); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, reference)
	}
	if len(rows) < 400 {
		t.Fatalf("incomplete C27 review references: %d", len(rows))
	}
	return rows
}

func TestC27ReviewPublicBoundaries(t *testing.T) {
	parallelBoundary(t)
	rows := c27ReviewReferences(t)
	generated, d2 := 0, 0
	for _, reference := range rows {
		t.Run(reference.Fn+"/"+reference.Test, func(t *testing.T) {
			args := reference.args(t)
			original := make([][]byte, len(args))
			for i, arg := range args {
				original[i] = ToJSON(arg)
			}
			got, err := callRuleDNSReference(t, reference.Fn, args)
			if reference.D2 != nil {
				d2++
				c27AssertD2(t, reference, args, original, got, err)
				return
			}
			if difference := compare(reference.row, got, err); difference != "" {
				t.Fatal(difference)
			}
			// This matrix's rejections are user-facing Chinese validation/conflict
			// messages, including Python ProfileValidationError (not SettingsError).
			if reference.Err != nil && err != nil && err.Error() != reference.Err[1] {
				t.Fatalf("message go=%q python=%q", err.Error(), reference.Err[1])
			}
			if len(reference.Usages) != 0 {
				var locked *RoutingLockedError
				if !errors.As(err, &locked) || !errors.Is(err, ErrRoutingLocked) {
					t.Fatalf("expected routing lock: %v", err)
				}
				want, err := FromJSON(reference.Usages)
				if err != nil {
					t.Fatal(err)
				}
				if difference := firstDifference(toNode(locked.Usages), want, "usages"); difference != "" {
					t.Fatal(difference)
				}
			}
			if difference := compareAfter(reference.row, args, got); difference != "" {
				t.Fatal(difference)
			}
			if reference.Fn == "apply_rule_dns_boundary" && err == nil && !isTrue(get(got, "generation_rejected")) {
				generated++
			}
		})
	}
	if len(rows) != 505 || d2 != 36 || generated != 314 {
		t.Fatalf("unexpected D2 scope: %d/%d, generated %d", d2, len(rows), generated)
	}
	t.Logf("%d strict references; %d final generated documents; %d explicit D2 outcomes", len(rows)-d2, generated, d2)
}

func c27AssertD2(t *testing.T, reference c27ReviewReference, args []*Node, original [][]byte, got *Node, err error) {
	t.Helper()
	d2 := reference.D2
	if d2.NativeKey == d2.LegacyKey || d2.Source == "" {
		t.Fatal("missing explicit D2 identity change")
	}
	for i, arg := range args {
		if !bytes.Equal(original[i], ToJSON(arg)) {
			t.Fatalf("D2 operation changed input %d", i)
		}
	}
	switch d2.Stage {
	case "catalog", "read":
		if err != nil {
			t.Fatal(err)
		}
		want, err := FromJSON(reference.Out)
		if err != nil {
			t.Fatal(err)
		}
		candidates := want
		if d2.Stage == "read" {
			candidates = get(want, "upstreams")
			set(get(want, "engine"), "editable", B(false))
			set(get(want, "engine"), "reason", S(messageDNSUpstreamKeyChanged))
			// The expired helper has no trustworthy source. Do not report the
			// heuristic's remote/fallback guess as an actual route.
			del(get(want, "rules"), "a")
		}
		replaced := 0
		for _, candidate := range items(candidates) {
			if isOneOf(get(candidate, "key"), d2.LegacyKey) {
				set(candidate, "key", S(d2.NativeKey))
				replaced++
			}
		}
		if replaced != 1 {
			t.Fatalf("expected exactly one changed EntryKey, got %d", replaced)
		}
		if difference := firstDifference(got, want, "D2"); difference != "" {
			t.Fatal(difference)
		}
		if d2.Stage == "read" {
			text, err := Generate(profileOf(args[0]), "https://panel.example.com")
			if err != nil {
				t.Fatal(err)
			}
			document, err := LoadNativeYAML(text, NativeLabel)
			if err != nil {
				t.Fatal(err)
			}
			want, err := FromJSON(d2.Delivered)
			if err != nil {
				t.Fatal(err)
			}
			if difference := firstDifference(document, want, "unchanged legacy delivery"); difference != "" {
				t.Fatal(difference)
			}
		}
	case "select":
		var setting *SettingsError
		if !errors.As(err, &setting) || setting.Msg != messageDNSStaleUpstream {
			t.Fatalf("old choice must fail as stale (400), got %v", err)
		}
	case "rebuild":
		var locked *RoutingLockedError
		if !errors.As(err, &locked) || !errors.Is(err, ErrRoutingLocked) || locked.Reason != messageDNSUpstreamKeyChanged {
			t.Fatalf("expired native graph must fail as a lock (409), got %v", err)
		}
		if len(locked.Usages) != 1 || locked.Usages[0].Reason != messageDNSUpstreamKeyChanged {
			t.Fatal("lock lost its user-facing reason")
		}
		assignments := []DNSAssignment{{"b", "refused"}}
		result, err := ApplyRuleDNS(profileOf(args[0]), profileOf(args[1]), assignments, RuleDNSApplyOptions{Lenient: true})
		if err != nil {
			t.Fatal(err)
		}
		if !same(result.Settings, get(args[1], "oxidns")) || result.Changed {
			t.Fatal("lenient D2 handling changed the old settings/YAML/source")
		}
		if len(result.Warnings) != 1 || result.Warnings[0] != "OxiDNS 分流含自定义结构，DNS 未修改："+messageDNSUpstreamKeyChanged {
			t.Fatal("lenient D2 handling lost the recovery warning")
		}
	default:
		t.Fatalf("unknown D2 assertion %q", d2.Stage)
	}
	for i, arg := range args {
		if !bytes.Equal(original[i], ToJSON(arg)) {
			t.Fatalf("D2 recovery check changed input %d", i)
		}
	}
}
