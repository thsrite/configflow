package difftest

import (
	"context"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestPythonHEADWriteIsFixed(t *testing.T) {
	w := newWorld(t, seed(t))
	before := snapshot(t, w.srv)
	// The same authorized HEAD with a JSON body writes in Flask, even though
	// HEAD is a read method. The Go owner deliberately runs its GET branch.
	request := map[string]any{"method": "HEAD", "path": "/api/rules/dns?profile=other", "body": `{"routes":{"inline":"blackhole"}}`, "headers": map[string]string{"Authorization": "Bearer " + w.token}}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(raw, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.scanner.Scan() {
		t.Fatal(w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		t.Fatal(err)
	}
	if py.Status != 200 || py.Error != "" {
		t.Fatalf("Python HEAD: %+v", py)
	}
	doc := py.State.(map[string]any)["document"].(map[string]any)
	profile := doc["profiles"].(map[string]any)["other"].(map[string]any)
	actions := profile["oxidns"].(map[string]any)["rule_actions"].(map[string]any)
	if actions["inline"].(map[string]any)["action"] != "blackhole" {
		t.Fatal("fixture no longer demonstrates Python HEAD write")
	}
	w.srv.Do("HEAD", "/api/rules/dns?profile=other", httpxtest.Bearer(w.token), httpxtest.Body([]byte(`{"routes":{"inline":"blackhole"}}`), "application/json")).WantStatus(200)
	if !reflect.DeepEqual(before, snapshot(t, w.srv)) {
		t.Fatal("Go HEAD wrote state")
	}
}
func TestMarkedDeliveryDoesNotAlterSource(t *testing.T) {
	for _, tc := range []struct{ name, behavior, format, content string }{
		{"classical-text", "classical", "text", "full:exact.example\n+.suffix.example\nDOMAIN,other.example\n192.0.2.0/24\n"},
		{"classical-yaml", "classical", "yaml", "payload: [full:exact.example, '+.suffix.example', 'DOMAIN,other.example']\n"},
		{"domain-yaml", "domain", "yaml", "payload: ['*.example.com', '+.other.example']\n"},
		{"ip-yaml", "ipcidr", "yaml", "payload: ['192.0.2.0/24', '2001:db8::/32']\n"},
		{"bom-cr", "domain", "text", "\ufeff*.example.com\r+.other.example\r"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("rule_library", append(p.GetList("rule_library"), jsonx.Obj("id", "delivery", "name", "Delivery", "source_type", "content", "behavior", tc.behavior, "format", tc.format, "content", tc.content)))
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			w := newWorld(t, setup)
			for _, suffix := range []string{"", "?target=mihomo", "?target=surge", "?target=Mihomo", ""} {
				w.call("GET", "/api/rules/local/Delivery"+suffix, "", false)
			}
		})
	}
}
func TestMatchingWildcardQuerySpellings(t *testing.T) {
	for _, pattern := range []string{"*.example.com", "+.example.com", "*", ".*"} {
		t.Run(pattern, func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.GetList("rule_library")[0].(*jsonx.Object).Set("content", pattern)
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("rule_configs", []any{jsonx.Obj("id", "wildcard", "itemType", "ruleset", "library_rule_id", "ads", "policy", "DIRECT")})
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			w := newWorld(t, setup)
			for _, query := range []string{"A.EXAMPLE.COM.", "_sip.example.com", "-host.example.com", "host-.example.com", "example.com.", "_service._tcp.example.com.", "LOCALHOST.", ".", "..", "example.com..", ".example.com", "a..example.com", "example.com\nother.com", "+.example.com", "::1.", "bad host", "https://example.com", "*.example.com", "127.0.0.1.", "300.1.1.1.", strings.Repeat("a", 64) + ".com"} {
				w.call("POST", "/api/rules/match-test?profile=other", body(map[string]string{"query": query}), true)
			}
		})
	}
}
func TestLargePolicyGroupAndDNSLimit(t *testing.T) {
	w := newWorld(t, seed(t))
	domains := make([]string, 250)
	ids := make([]string, 250)
	for i := range domains {
		domains[i] = "node" + strings.Repeat("a", i%20) + string(rune('a'+i/20)) + ".example"
		ids[i] = "rule_" + strconv.Itoa(i+1)
	}
	w.call("POST", "/api/rules/batch?profile=other", body(map[string]any{"rule_type": "DOMAIN-SUFFIX", "domains": domains, "policy": "DIRECT"}), true)
	w.call("PUT", "/api/rules/policy?profile=other", body(map[string]any{"ids": ids, "policy": "PROXY"}), true)
	routes := map[string]string{}
	for _, id := range ids[:201] {
		routes[id] = "direct"
	}
	w.call("PUT", "/api/rules/dns?profile=other", body(map[string]any{"routes": routes}), true)
}

func TestAcceptedSourceLineBoundaries(t *testing.T) {
	for _, separator := range []string{"\n", "\r\n", "\r", "\v", "\f", "\x1c", "\x1d", "\x1e", "\u0085", "\u2028", "\u2029"} {
		t.Run(body(separator), func(t *testing.T) {
			setup := func(ctx context.Context, r *repo.Repository) {
				seed(t)(ctx, r)
				_, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.GetList("rule_library")[0].(*jsonx.Object).Set("content", strings.Join([]string{"*.example.com", "+.other.example", ""}, separator))
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				_, _, err = r.UpdateProfileTransaction(ctx, "other", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
					p.Set("rule_configs", []any{jsonx.Obj("id", "set", "itemType", "ruleset", "library_rule_id", "ads", "policy", "DIRECT")})
					return nil, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			w := newWorld(t, setup)
			w.call("GET", "/api/profiles/other/rules/local/Ads?target=mihomo", "", false)
			for _, query := range []string{"a.example.com", "other.example", "a.b.example.com"} {
				w.call("POST", "/api/rules/match-test?profile=other", body(map[string]string{"query": query}), true)
			}
		})
	}
}
