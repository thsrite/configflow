package onboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/yamlx"
)

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
			if os.Getenv("CONFIGFLOW_DIFF") != "1" {
				return
			}
			input, _ := json.Marshal(struct {
				Source string       `json:"source"`
				Hosts  []netip.Addr `json:"hosts"`
			}{tc.source, tc.hosts})
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", `import json,sys;from backend.common.gateway_onboarding import _prepare_mihomo_dns,GatewayOnboardingError
x=json.load(sys.stdin)
try:
 _prepare_mihomo_dns({'mihomo':{'custom_config':x['source']}},local_addresses=x['hosts'] or []);json.dump({'blocked':False},sys.stdout)
except GatewayOnboardingError as e: json.dump({'blocked':True,'message':str(e)},sys.stdout,ensure_ascii=False)`)
			cmd.Dir = "../../../.."
			cmd.Env = append(os.Environ(), "PYTHONPATH=.")
			cmd.Stdin = bytes.NewReader(input)
			out, pyerr := cmd.CombinedOutput()
			if pyerr != nil {
				t.Fatalf("Python loop: %v %s", pyerr, out)
			}
			var expected struct {
				Blocked bool   `json:"blocked"`
				Message string `json:"message"`
			}
			if e := json.Unmarshal(out, &expected); e != nil || expected.Blocked != tc.blocked {
				t.Fatalf("Python boundary %s %v", out, e)
			}
			if err != nil && err.Error() != expected.Message {
				t.Fatalf("Chinese error Go=%v Python=%s", err, expected.Message)
			}
		})
	}
	s := testService(t)
	s.DNSLoop = CheckDNSLoop
	if _, err := s.Execute(context.Background(), testOperation()); err != nil {
		t.Fatal(err)
	}
	profile, _, err := s.Repository.GetPublishSnapshot(context.Background(), "default", true)
	if err != nil {
		t.Fatal(err)
	}
	native, err := fromLegacy(profile)
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
