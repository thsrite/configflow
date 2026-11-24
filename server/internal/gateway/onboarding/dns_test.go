package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
)

func noLoop(*yamlx.Node, []netip.Addr) (bool, error) { return false, nil }

func normalized(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for key, value := range x {
				if s, ok := value.(string); ok && (key == "custom_config" || key == "native_config") && s != "" {
					node, err := yamlx.Parse([]byte(s))
					if err == nil {
						b, err := yamlx.ToJSON(node)
						if err != nil {
							t.Fatal(err)
						}
						var parsed any
						if err = json.Unmarshal(b, &parsed); err != nil {
							t.Fatal(err)
						}
						x[key] = walk(parsed)
						continue
					}
				}
				x[key] = walk(value)
			}
		case []any:
			for i, value := range x {
				x[i] = walk(value)
			}
		}
		return v
	}
	return walk(value)
}

func equalJSON(t *testing.T, label string, got, want json.RawMessage) {
	t.Helper()
	a, b := normalized(t, got), normalized(t, want)
	if !reflect.DeepEqual(a, b) {
		ga, _ := json.Marshal(a)
		wa, _ := json.Marshal(b)
		t.Errorf("%s\ngot  %s\nwant %s", label, ga, wa)
	}
}

func TestSharedDNSPreparationPreservesBase(t *testing.T) {
	for _, source := range []string{"", "future: &future {keep: true}\nreference: *future\ndns: {}\n", "future: keep\ndns:\n  nameserver: [tls://192.0.2.53]\n"} {
		var document *yamlx.Node
		var err error
		if source != "" {
			document, err = yamlx.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
		}
		prepared, err := PrepareMihomoDNS(document, nil, noLoop)
		if err != nil {
			t.Fatal(err)
		}
		resolvers := stringList(field(prepared, "dns", "nameserver"))
		if len(resolvers) == 0 {
			t.Fatal("missing nameservers")
		}
		if source != "" && field(prepared, "future") == nil {
			t.Fatal("discarded base field")
		}
		if source != "" && field(prepared, "reference") != nil && yamlx.Resolve(field(prepared, "reference")) != yamlx.Resolve(field(prepared, "future")) {
			t.Fatal("expanded shared alias")
		}
		if source == "future: keep\ndns:\n  nameserver: [tls://192.0.2.53]\n" && resolvers[0] != "tls://192.0.2.53" {
			t.Fatal("overwrote existing resolver")
		}
	}
	a := DefaultMihomoNameservers()
	a[0] = "changed"
	if DefaultMihomoNameservers()[0] != DefaultLocalDNS {
		t.Fatal("mutable shared defaults")
	}
	if _, err := PrepareMihomoDNS(nil, nil, nil); err == nil {
		t.Fatal("missing shared checker allowed")
	}
}

// TestRealDNSLoopPreparation uses the reviewed shared parser at the leaf and
// installation guard, with a normal resolver and representative loop sources.
func TestRealDNSLoopPreparation(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		hosts        []netip.Addr
		blocked      bool
	}{
		{"remote", "dns:\n  nameserver: [tls://192.0.2.53]\n", nil, false},
		{"loopback", "dns:\n  nameserver: [udp://127.0.0.1:1053]\n", nil, true},
		{"target-host", "dns:\n  nameserver: [tls://192.0.2.53]\n  fallback: ['tcp://[2001:db8::53]:53']\n", []netip.Addr{netip.MustParseAddr("2001:db8::53")}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			document, err := yamlx.Parse([]byte(tc.source))
			if err != nil {
				t.Fatal(err)
			}
			_, err = PrepareMihomoDNS(document, tc.hosts, CheckDNSLoop)
			if (err != nil) != tc.blocked {
				t.Fatalf("shared checker blocked=%v err=%v", tc.blocked, err)
			}
		})
	}
	s := testService(t)
	s.DNSLoop = CheckDNSLoop
	if _, err := s.Execute(context.Background(), testOperation()); err != nil {
		t.Fatal(err)
	}
	profile, _, err := s.Repository.PublishConfig(context.Background(), "default", true)
	if err != nil {
		t.Fatal(err)
	}
	native, err := modelDocument(profile)
	if err != nil {
		t.Fatal(err)
	}
	// AssertReady defaults to the same shared checker; a target host address in
	// any DNS field must reject installation without mutating the saved profile.
	if _, err := AssertReady(context.Background(), native, Options{BaseURL: "https://panel.example"}); err != nil {
		t.Fatal(err)
	}
	mihomo := object(native["mihomo"])
	mihomo["custom_config"] = encode("dns:\n  nameserver: [tcp://192.0.2.53:53]\n")
	native["mihomo"] = encode(mihomo)
	before := encode(native)
	if _, err := AssertReady(context.Background(), native, Options{BaseURL: "https://panel.example", LocalAddresses: []netip.Addr{netip.MustParseAddr("192.0.2.53")}}); err == nil {
		t.Fatal("installation ignored target-host loop")
	}
	if !bytes.Equal(before, encode(native)) {
		t.Fatal("readiness mutated saved document")
	}
}
