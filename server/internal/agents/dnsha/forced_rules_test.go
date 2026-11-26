package dnsha

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/rulefetch"
)

func TestManualPairRulesForcesUnchangedLibraryMaterialization(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	var content atomic.Value
	content.Store("first.example\n")
	var fetches atomic.Int32
	ruleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, content.Load().(string))
	}))
	t.Cleanup(ruleServer.Close)
	err := f.r.UpdateShared(f.ctx, func(_ context.Context, shared *model.Shared) error {
		raw, err := json.Marshal([]map[string]any{{
			"id":          "library-example",
			"name":        "Synthetic library",
			"source_type": "url",
			"url":         ruleServer.URL + "/rules.txt",
			"behavior":    "domain",
			"format":      "text",
		}})
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, &shared.RuleLibrary)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.r.UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		rulesRaw, err := json.Marshal([]map[string]any{{
			"id":              "ruleset-example",
			"itemType":        "ruleset",
			"library_rule_id": "library-example",
			"policy":          "DIRECT",
			"enabled":         true,
		}})
		if err != nil {
			return err
		}
		if err := p.SetJSON("rule_configs", rulesRaw); err != nil {
			return err
		}
		oxidnsRaw, err := json.Marshal(map[string]any{
			"config_mode":     "template",
			"direct_rulesets": []string{"ruleset-example"},
			"local_dns":       "udp://192.0.2.1:53",
			"remote_dns":      "udp://192.0.2.2:53",
		})
		if err != nil {
			return err
		}
		return p.SetJSON("oxidns", oxidnsRaw)
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	source, revision, err := f.s.source(f.ctx, owner, true)
	if err != nil {
		t.Fatal(err)
	}
	dp := deploy.New(deploy.Options{Pair: func(ctx context.Context, _ *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
		return f.s.PublishPair(ctx, id, b, o)
	}})
	prepared, err := dp.PrepareContent(f.ctx, f.m, owner.target, source, deploy.PrepareOptions{BaseURL: "https://panel.example", DeploymentID: "remote-rules-baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dp.Publish(f.ctx, f.m, "primary", prepared.Bundle, deploy.PublishOptions{Activate: true, BaseURL: "https://panel.example", SourceSnapshot: source, ResourceConfig: source, ConfigRevision: revision}); err != nil {
		t.Fatal(err)
	}
	if last := f.drain(t, 12); last.Status != "succeeded" {
		t.Fatalf("baseline: %s", last.Message)
	}
	previous := fetches.Load()
	if previous == 0 {
		t.Fatal("baseline did not download the actual remote library")
	}
	before := len(f.uploads())
	// The library scheduler refreshes its cache. The saved source remains
	// unchanged, so the manual owner must force materialization instead of
	// pinning the identical declaration's old deployment bytes, as Python does.
	content.Store("second.example\n")
	raw, err := source.Raw()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := model.DecodeConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	library := cfg.RuleLibrary[0]
	_, stale, err := refresh.New(refresh.Options{Repo: f.m.Repository()}).RefreshRemote(f.ctx, library, "manual", refresh.RuleReadOptions{
		Config: &rulefetch.SourceConfig{
			Settings: cfg.SystemConfig,
			Library:  cfg.RuleLibrary,
		},
	})
	if err != nil || stale != nil {
		t.Fatalf("library refresh: %v / %v", err, stale)
	}
	if fetches.Load() <= previous {
		t.Fatal("library scheduler did not refresh the remote bytes")
	}
	if _, _, err = f.s.PairRules(f.ctx, "primary", "POST"); err != nil {
		t.Fatal(err)
	}
	if last := f.drain(t, 12); last.Status != "succeeded" {
		t.Fatalf("rule publication: %s", last.Message)
	}
	uploads := f.uploads()
	if len(uploads) != before+2 {
		t.Fatal("rule refresh did not publish both members")
	}
	for _, upload := range uploads[before:] {
		gz, err := gzip.NewReader(bytes.NewReader(upload.Body))
		if err != nil {
			t.Fatal(err)
		}
		reader := tar.NewReader(gz)
		found := false
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(header.Name, "files/rules/") {
				continue
			}
			data, err := io.ReadAll(reader)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(data, []byte("second.example")) {
				found = true
			}
			if bytes.Contains(data, []byte("first.example")) {
				t.Fatal("peer received pinned stale rule bytes")
			}
		}
		_ = gz.Close()
		if !found {
			t.Fatal("delivered pair bundle lacked the freshly downloaded library")
		}
	}
}
