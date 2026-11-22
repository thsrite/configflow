package main

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// docFrom builds a document from JSON text.
func docFrom(t *testing.T, text string) *Map {
	t.Helper()
	v, err := loadJSON([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	return v.(*Map)
}

func routeRows(method string, routes ...string) [][]string {
	var rows [][]string
	for _, r := range routes {
		rows = append(rows, []string{method, r})
	}
	return rows
}

func TestEnumeratesEverySnapshotEntity(t *testing.T) {
	doc := docFrom(t, `{"system":{"agents":[{"id":"agent-a"},{"id":"agent-b"}]},
		"shared":{"subscriptions":[{"id":"sub-a"},{"id":"sub-b"}],"rule_library":[]},
		"profiles":{"one":{"routers":{"items":[{"id":"router-a"}]}},"two":{"routers":{"items":[]}}}}`)
	rows := routeRows("GET", "/api/agents/<agent_id>", "/api/subscriptions/<sub_id>/nodes", "/api/routers/<router_id>/routes")
	cases, err := enumerate(rows, nil, doc, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	// 2 profiles x entities, plus one default-profile request per entity of the first profile.
	if len(cases) != 15 {
		t.Fatalf("%d cases", len(cases))
	}
	noData, router, unscoped := 0, false, 0
	for _, c := range cases {
		if c.NoData {
			noData++
		}
		if strings.Contains(c.Path, "/router-a/") {
			router = true
		}
		if !strings.Contains(c.Path, "profile_id=") {
			unscoped++
		}
	}
	if noData != 1 || !router || unscoped != 5 {
		t.Fatalf("noData=%d router=%v unscoped=%d", noData, router, unscoped)
	}
}

func TestHTTPSubscriptionTakeoverRequestsAllFormats(t *testing.T) {
	doc := docFrom(t, `{"system":{"agents":[]},"shared":{"subscriptions":[{"id":"sub-a"}]},"profiles":{"one":{"routers":{"items":[]}}}}`)
	cases, err := enumerate(routeRows("GET", "/api/profiles/<profile_id>/subscriptions/<sub_id>/proxies"), nil, doc, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	formats := map[string]bool{}
	for _, c := range cases {
		u, _ := url.Parse(c.Path)
		formats[u.Query().Get("format")] = true
	}
	if len(cases) != 3 || len(formats) != 3 || !formats["mihomo"] || !formats["surge"] || !formats["loon"] {
		t.Fatalf("%v", cases)
	}
}

func TestAggregationCatalogSelectorsUseSharedKey(t *testing.T) {
	doc := docFrom(t, `{"shared":{"subscription_aggregations":[{"id":"one"},{"id":"two"}]},"profiles":{"default":{},"other":{}},"system":{}}`)
	cases, err := enumerate(routeRows("GET", "/api/aggregations/<agg_id>/count", "/api/aggregations/<resource_id>/health-impact"), nil, doc, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 12 {
		t.Fatalf("%d cases", len(cases))
	}
	for _, c := range cases {
		if strings.Contains(c.Path, "proddiff-missing") {
			t.Fatalf("missing parameter in %s", c.Path)
		}
	}
}

func TestEnumerationBodiesAndQueriesFollowTheSnapshot(t *testing.T) {
	doc := docFrom(t, `{"system":{"agents":[{"id":"a b","name":"Gate way","host":"192.0.2.9","service_type":"oxidns","port":53,"token":"t/1","profile_id":"two"}],
		"system_config":{"server_domain":"https://panel.example"}},
		"shared":{"subscriptions":[{"id":"s1"}],"oxidns":{"native_config":"x: 1"}},
		"profiles":{"one":{},"two":{}}}`)
	readonly := map[string]bool{"/api/agents/<agent_id>/generate-config": true, "/api/oxidns/render": true, "/api/subscriptions/preview-regex": true}
	rows := append(routeRows("POST", "/api/agents/<agent_id>/generate-config", "/api/oxidns/render", "/api/subscriptions/preview-regex", "/api/not-listed"),
		routeRows("GET", "/api/agents/<agent_id>/config", "/api/agents/install-script")...)
	cases, err := enumerate(rows, readonly, doc, "/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	byRoute := map[string][]testCase{}
	for _, c := range cases {
		byRoute[c.Route] = append(byRoute[c.Route], c)
	}
	if len(byRoute["/api/not-listed"]) != 0 {
		t.Fatal("unlisted POST enumerated")
	}
	generate := byRoute["/api/agents/<agent_id>/generate-config"][0]
	if generate.Path != "/api/agents/a%20b/generate-config?profile_id=one" {
		t.Fatalf("path %s", generate.Path)
	}
	if got := dumpJSON(generate.Body, dumpOptions{indent: -1}); got != `{"base_url": "http://127.0.0.1:5001", "expected_profile_id": "two", "expected_service_type": "oxidns"}` {
		t.Fatalf("body %s", got)
	}
	render := byRoute["/api/oxidns/render"][0]
	if got := dumpJSON(render.Body, dumpOptions{indent: -1}); got != `{"base_url": "http://127.0.0.1:5001", "settings": {"native_config": "x: 1"}, "changes": {}, "native_config": "x: 1"}` {
		t.Fatalf("body %s", got)
	}
	if got := dumpJSON(byRoute["/api/subscriptions/preview-regex"][0].Body, dumpOptions{indent: -1}); !strings.Contains(got, `"source_id": "s1", "regex": ".*"`) {
		t.Fatalf("body %s", got)
	}
	config := byRoute["/api/agents/<agent_id>/config"]
	if len(config) != 3 || config[0].Path != "/api/agents/a%20b/config?profile_id=one&token=t%2F1" || config[1].Path != "/api/agents/a%20b/config?token=t%2F1" {
		t.Fatalf("config %v", config)
	}
	install := byRoute["/api/agents/install-script"][0]
	want := "/api/agents/install-script?profile_id=one&server_url=https%3A%2F%2Fpanel.example&name=Gate+way&agent_name=Gate+way&agent_ip=192.0.2.9&type=oxidns&port=53&enable_mihomo=true&enable_oxidns=true"
	if install.Path != want {
		t.Fatalf("install %s", install.Path)
	}
}

func TestStoredIdentifiersComeFromLocalMetadata(t *testing.T) {
	data := t.TempDir()
	write := func(path, text string) {
		full := filepath.Join(data, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(".generated-previews/p1/record.json", `{"id":"p1","bundle_meta":{"manifest":{"files":[{"path":"config.yaml"},{"path":"rules/a.txt"}]}}}`)
	write(".generated-previews/p2/record.json", `{"id":"p2"}`)
	write("probes/j.json", `{"id":"job-1"}`)
	write("probes/broken.json", `{`)
	doc := docFrom(t, `{"system":{"agents":[{"id":"a","deployments":{"d1":{}}}]},"shared":{},"profiles":{"one":{}}}`)
	rows := routeRows("GET", "/api/previews/<preview_id>/file", "/api/probes/<job_id>", "/api/deployments/<deployment_id>")
	cases, err := enumerate(rows, nil, doc, data)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, c := range cases {
		if strings.Contains(c.Path, "profile_id=") {
			paths = append(paths, c.Path)
		}
	}
	want := []string{
		"/api/previews/p1/file?profile_id=one&path=config.yaml",
		"/api/previews/p1/file?profile_id=one&path=rules%2Fa.txt",
		"/api/previews/p2/file?profile_id=one&path=surge.conf",
		"/api/probes/job-1?profile_id=one",
		"/api/deployments/d1?profile_id=one",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths:\n%s", strings.Join(paths, "\n"))
	}
}
