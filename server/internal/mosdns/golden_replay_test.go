package mosdns_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/localhttp"
	"configflow/internal/model"
	"configflow/internal/repo/repotest"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
)

const (
	mosDomain   = "http://127.0.0.1:5001"
	mosCfgToken = "diff-config-token"
	mosRetired  = "diff-retired-rule-token"
)

var mosLocalPortRe = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

type mosTestSpawner struct{}

func (mosTestSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	go fn(ctx)
	return nil
}

func normalizeMosGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if strings.HasSuffix(k, "_at") || k == "last_duration_ms" {
				if val != nil {
					out[k] = "<set>"
					continue
				}
			}
			out[k] = normalizeMosGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeMosGolden(item)
		}
		return out
	case string:
		return mosLocalPortRe.ReplaceAllString(x, "http://127.0.0.1:<port>")
	}
	return v
}

func TestMosDNSRoutesGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/list.txt":
			_, _ = io.WriteString(w, "+.new.test\nexample.test\n")
		case "/classical.txt":
			_, _ = io.WriteString(w, "DOMAIN,exact.test\nDOMAIN-SUFFIX,suffix.test\nIP-CIDR,192.0.2.0/24,no-resolve\n")
		case "/bad.txt":
			_, _ = io.WriteString(w, "DOMAIN,bad.test\n")
		case "/down.txt":
			http.Error(w, "down", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)

	s, _ := servertest.New(t, httpxtest.Options{
		Auth: true,
	})
	localhttp.Register(s.Handler(), mosTestSpawner{})
	t.Cleanup(func() { localhttp.Register(nil, nil) })
	ctx := s.Context()

	if err := s.Repo.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.SystemConfig.ServerDomain = mosDomain
		sys.SystemConfig.ConfigToken = mosCfgToken
		sys.SystemConfig.RetiredRuleProxyTokens = []string{mosRetired}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	library := []any{
		map[string]any{"id": "inline", "name": "Inline", "source_type": "content", "content": "+.inline.test\nDOMAIN,exact.inline.test", "behavior": "classical", "format": "text"},
		map[string]any{"id": "remote", "name": "Remote", "source_type": "url", "url": up.URL + "/list.txt", "behavior": "domain", "format": "text"},
		map[string]any{"id": "classical", "name": "Classical", "source_type": "url", "url": up.URL + "/classical.txt", "behavior": "classical", "format": "text"},
		map[string]any{"id": "bad", "name": "Bad", "source_type": "url", "url": up.URL + "/bad.txt", "behavior": "domain", "format": "text"},
		map[string]any{"id": "binary", "name": "Binary", "source_type": "url", "url": up.URL + "/list.mrs", "behavior": "domain", "format": "mrs"},
		map[string]any{"id": "inline-off", "name": "InlineOff", "source_type": "content", "content": "+.off.test", "behavior": "domain", "format": "text"},
	}
	if err := s.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		raw, _ := json.Marshal(shared.RuleLibrary)
		var items []any
		_ = json.Unmarshal(raw, &items)
		items = append(items, library...)
		merged, _ := json.Marshal(items)
		return shared.SetJSON("rule_library", merged)
	}); err != nil {
		t.Fatal(err)
	}
	var stored json.RawMessage
	if _, err := s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		raw, _ := json.Marshal(profile.RuleConfigs)
		var configs []any
		_ = json.Unmarshal(raw, &configs)
		for _, id := range []string{"inline", "remote", "classical", "bad", "binary"} {
			configs = append(configs, map[string]any{"id": "sel-" + id, "itemType": "ruleset", "library_rule_id": id, "policy": "DIRECT", "enabled": true})
		}
		configs = append(configs,
			map[string]any{"id": "sel-inline-off", "itemType": "ruleset", "library_rule_id": "inline-off", "policy": "DIRECT", "enabled": nil},
			map[string]any{"id": "odd", "itemType": "rule", "rule_type": "DOMAIN", "value": "odd.test", "policy": "DIRECT", "library_rule_id": 7, "enabled": "yes"},
		)
		var err error
		stored, err = json.Marshal(configs)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	repotest.WriteRow(t, s.Repo.DatabasePath(), "profile", "default", "rule_configs", stored)

	sys, err := s.Repo.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ruleTok := sys.SystemConfig.RuleProxyToken
	admin := s.Login()

	type req struct {
		name, method, path, body string
		anonymous                bool
		authHeader               string
	}
	q := func(pairs ...string) url.Values {
		v := url.Values{}
		for i := 0; i < len(pairs); i += 2 {
			v.Set(pairs[i], pairs[i+1])
		}
		return v
	}
	proxy := func(name, path string, query url.Values, anonymous bool, authHeader string) req {
		return req{name: name, method: "GET", path: path + "?" + query.Encode(), anonymous: anonymous, authHeader: authHeader}
	}
	const rp = "/api/mosdns/rule-proxy"
	const prp = "/api/profiles/default/mosdns/rule-proxy"
	inlineURL := mosDomain + "/api/profiles/default/rule-library/content/inline"

	reqs := []req{}
	for _, route := range []string{"rulesets", "custom-matches", "dns-servers", "log-settings", "api-settings", "cache-settings"} {
		reqs = append(reqs, req{name: "get-" + route, method: "GET", path: "/api/mosdns/" + route})
	}
	reqs = append(reqs,
		req{name: "post-rulesets", method: "POST", path: "/api/mosdns/rulesets", body: `{"direct_rulesets":["sel-inline"],"proxy_rulesets":["sel-remote"]}`},
		req{name: "post-custom-matches", method: "POST", path: "/api/mosdns/custom-matches", body: `{"custom_matches":["qname domain:a.test"],"position":"head"}`},
		req{name: "get-custom-matches-after", method: "GET", path: "/api/mosdns/custom-matches"},
		req{name: "post-custom-matches-empty", method: "POST", path: "/api/mosdns/custom-matches", body: `{}`},
		req{name: "post-dns-servers", method: "POST", path: "/api/mosdns/dns-servers", body: `{"local_dns":"223.5.5.5","remote_dns":"tls://1.1.1.1","custom_hosts":"a.test 192.0.2.1"}`},
		req{name: "post-log-settings", method: "POST", path: "/api/mosdns/log-settings", body: `{"log_enabled":false,"log_level":"debug","log_file":"m.log"}`},
		req{name: "post-api-settings", method: "POST", path: "/api/mosdns/api-settings", body: `{"api_enabled":false,"api_address":":9090"}`},
		req{name: "post-cache-settings-ascii", method: "POST", path: "/api/mosdns/cache-settings", body: `{"cache_size":"128","cache_lazy_ttl":"60","cache_dump_interval":"20","cache_enabled":true,"cache_dump_file":"cache.dump"}`},
		req{name: "post-cache-settings-null", method: "POST", path: "/api/mosdns/cache-settings", body: `null`},
		req{name: "post-settings", method: "POST", path: "/api/mosdns/settings", body: `{"custom_config":"plugins: []","direct_rulesets":["sel-classical"],"local_dns":"119.29.29.29","cache_size":256,"api_address":":8338","unknown":1}`},
		req{name: "post-settings-profile", method: "POST", path: "/api/mosdns/settings?profile=default", body: `{"log_level":"warn"}`},
		req{name: "post-settings-not-object", method: "POST", path: "/api/mosdns/settings", body: `[1]`},
		req{name: "post-settings-missing-rule", method: "POST", path: "/api/mosdns/settings", body: `{"proxy_rules":["missing"]}`},
		req{name: "post-settings-unknown-profile", method: "POST", path: "/api/mosdns/settings?profile=nope", body: `{}`},
		proxy("proxy-inline", rp, q("url", inlineURL, "token", ruleTok, "part", "domain"), true, ""),
		proxy("proxy-inline-cfgtok", rp, q("url", inlineURL, "token", mosCfgToken), true, ""),
		proxy("proxy-inline-jwt", prp, q("url", inlineURL, "part", "ip"), false, ""),
		proxy("proxy-remote-declared", rp, q("url", up.URL+"/list.txt", "source_id", "remote", "token", ruleTok), true, ""),
		proxy("proxy-remote-cached", prp, q("url", up.URL+"/list.txt", "source_id", "remote", "token", ruleTok, "part", "domain"), true, ""),
		proxy("proxy-classical-ip", rp, q("url", up.URL+"/classical.txt", "token", ruleTok, "part", "ip"), true, ""),
		proxy("proxy-invalid-content", rp, q("url", up.URL+"/bad.txt", "source_id", "bad", "token", ruleTok), true, ""),
		proxy("proxy-mrs", rp, q("url", up.URL+"/list.mrs", "source_id", "binary", "token", ruleTok), true, ""),
		proxy("proxy-undeclared-down", rp, q("url", up.URL+"/down.txt", "token", ruleTok), true, ""),
		proxy("proxy-changed-source", rp, q("url", up.URL+"/moved.txt", "source_id", "remote", "token", ruleTok), true, ""),
		proxy("proxy-no-token", rp, q("url", inlineURL), true, ""),
		proxy("proxy-wrong-token", prp, q("url", inlineURL, "token", "wrong"), true, ""),
		proxy("proxy-no-url", rp, q("token", ruleTok), true, ""),
		proxy("proxy-bad-part", rp, q("url", inlineURL, "token", ruleTok, "part", "both"), true, ""),
		proxy("proxy-ftp", rp, q("url", "ftp://127.0.0.1/rules", "token", ruleTok), true, ""),
		proxy("proxy-retired-url-tok", rp, q("url", inlineURL, "token", mosRetired), true, ""),
		proxy("proxy-rule-tok-bearer", rp, q("url", inlineURL), true, "Bearer "+ruleTok),
		proxy("proxy-inline-disabled", rp, q("url", mosDomain+"/api/profiles/default/rule-library/content/inline-off", "token", ruleTok), true, ""),
		proxy("proxy-unknown-profile", "/api/profiles/nope/mosdns/rule-proxy", q("url", inlineURL, "token", ruleTok), true, ""),
	)

	for _, r := range reqs {
		t.Run(r.name, func(t *testing.T) {
			var opts []httpxtest.Option
			if !r.anonymous {
				opts = append(opts, admin)
			} else if r.authHeader != "" {
				opts = append(opts, httpxtest.Header("Authorization", r.authHeader))
			}
			if r.body != "" {
				opts = append(opts, httpxtest.Body([]byte(r.body), "application/json"))
			}
			resp := s.Do(r.method, r.path, opts...)
			var parsed any
			if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
				_ = json.Unmarshal(resp.Body, &parsed)
			} else {
				parsed = string(resp.Body)
			}
			key, _ := json.Marshal(map[string]string{"case": r.name})
			actual, _ := json.Marshal(normalizeMosGolden(map[string]any{
				"status":       resp.Status,
				"content_type": resp.Header.Get("Content-Type"),
				"cache_hdr":    resp.Header.Get("X-ConfigFlow-Rule-Cache"),
				"body":         parsed,
			}))
			ref.Assert(t, key, actual)
		})
	}
}
