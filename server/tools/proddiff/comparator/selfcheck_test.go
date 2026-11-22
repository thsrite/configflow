package main

import (
	"errors"
	"strings"
	"testing"
)

func selfCheckSamples(t *testing.T) (reply, map[string]reply) {
	samples := map[string]reply{
		"yaml":      bodyReply(200, "text/yaml", []byte("dns:\n  upstreams:\n  - port: 53\nmode: rule\n")),
		"text":      bodyReply(200, "text/plain", []byte("[General]\nloglevel=info\n[Rule]\nFINAL,DIRECT\n")),
		"headers":   rep([]byte("{}"), jsonType, 200, map[string]string{"x-configflow-profile": "one", "date": "x"}),
		"http-json": jrep(t, `{"success":true,"data":{"items":[{"id":"a","port":1},{"id":"b","port":2}]}}`),
	}
	real := rep([]byte(`{"auth_enabled":true}`), jsonType, 200, map[string]string{"date": "x"})
	return real, samples
}

func TestSelfCheckUsesRealSamplesAndRejectsBlindComparators(t *testing.T) {
	c := testComparator(t)
	real, samples := selfCheckSamples(t)
	checks, err := c.selfCheck(real, samples)
	if err != nil {
		t.Fatal(err)
	}
	if len(checks) != 23 {
		t.Fatalf("%d checks: %v", len(checks), checks)
	}
	passed := map[string]bool{}
	for _, name := range checks {
		passed[name] = true
	}
	for _, name := range []string{"status", "header-value", "header-missing", "json-real-leaf", "json-missing-key", "json-extra-key",
		"json-null-vs-value", "json-real-list-order", "json-int-float", "json-duplicate-key", "yaml-leaf",
		"yaml-duplicate-key", "control-yaml-redumped", "text-line", "text-insert", "text-line-endings",
		"transport-both", "control-json-reencoded"} {
		if !passed[name] {
			t.Fatalf("%s missing", name)
		}
	}
	for name, blind := range map[string]compareFunc{
		"never":  func(reply, reply) []diff { return nil },
		"always": func(reply, reply) []diff { return []diff{{"body", "value"}} },
	} {
		if _, err := selfCheck(real, samples, blind, c.keys); !errors.Is(err, errSelfCheckFailed) {
			t.Fatalf("%s comparator passed: %v", name, err)
		}
	}
	for missing := range samples {
		partial := map[string]reply{}
		for k, v := range samples {
			if k != missing {
				partial[k] = v
			}
		}
		if _, err := c.selfCheck(real, partial); !errors.Is(err, errSelfCheckSample) {
			t.Fatalf("without %s: %v", missing, err)
		}
	}
}

func TestYAMLRedumpControlKeepsRealResponseMetadataAndRejectsLoss(t *testing.T) {
	c := testComparator(t)
	real, base := selfCheckSamples(t)
	samples := map[string]reply{"text": base["text"], "headers": rep([]byte("{}"), jsonType, 200, map[string]string{"cache-control": "no-store"}),
		"http-json": jrep(t, `{"items":[{"id":"a"},{"id":"b"}]}`)}
	for _, tt := range []struct {
		contentType string
		status      int
	}{{"text/yaml", 200}, {"application/yaml; charset=utf-8", 200}, {"application/x-yaml", 201}} {
		samples["yaml"] = rep([]byte("dns: {port: 53}\nenabled: true\n"), tt.contentType, tt.status, nil)
		checks, err := c.selfCheck(real, samples)
		if err != nil {
			t.Fatalf("%s: %v", tt.contentType, err)
		}
		joined := strings.Join(checks, ",")
		for _, name := range []string{"control-yaml-redumped", "yaml-leaf", "yaml-duplicate-key"} {
			if !strings.Contains(joined, name) {
				t.Fatalf("%s: %s missing", tt.contentType, name)
			}
		}
	}
	// A tagged ordered mapping cannot be a formatting-only control.
	samples["yaml"] = rep([]byte("ordered: !!omap\n- alpha: 1\n- beta: 2\n"), "application/yaml", 200, nil)
	if _, err := c.selfCheck(real, samples); err == nil || !strings.Contains(err.Error(), "control-yaml-redumped") {
		t.Fatalf("omap control: %v", err)
	}
	// A document with duplicate keys is consumer-rejected, never a control.
	samples["yaml"] = rep([]byte("dns: {port: 53, port: 54}\n"), "application/yaml", 200, nil)
	if _, err := c.selfCheck(real, samples); err == nil || !strings.Contains(err.Error(), "control-yaml-redumped") {
		t.Fatalf("duplicate control: %v", err)
	}
	// Fixing the control's metadata must not make real metadata changes equal.
	if found := diffsOf(t, c, rep([]byte("dns: {port: 53}\n"), "application/yaml", 200, nil), rep([]byte("dns: {port: 53}\n"), "text/yaml", 200, nil), "/self-check", nil); !contains(found, diff{"Content-Type", "value"}) {
		t.Fatal(found)
	}
}
