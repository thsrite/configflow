package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestHealthCoreHasNoLegacyOrUntypedDependency(t *testing.T) {
	coreFiles := []string{"messages.go", "values.go", "conversion.go", "wire.go", "ipaddr.go", "runner.go", "dns.go", "latency.go"}
	forbiddenImports := []string{"jsonx", "pystr", "internal/subs", "internal/substore"}
	forbiddenNames := map[string]bool{
		"any":           true,
		"Env":           true,
		"Settings":      true,
		"pyTypeError":   true,
		"typeName":      true,
		"subscript":     true,
		"pyList":        true,
		"pythonOSError": true,
		"osError":       true,

		"DefaultEnv":          true,
		"SetEnv":              true,
		"CurrentEnv":          true,
		"FindProbeBinary":     true,
		"NormalizeDNSServers": true,
		"NormalizeConfig":     true,
		"LoadConfig":          true,
		"EffectiveSettings":   true,
		"DescribeSettings":    true,
		"SaveConfig":          true,
		"CheckSettings":       true,
		"DNSSettings":         true,

		"CollectCandidates":    true,
		"LoadResults":          true,
		"RunTests":             true,
		"toRawJSON":            true,
		"encodeToLegacyValue":  true,
		"encodeToLegacyObject": true,
		"encodeToLegacySlice":  true,
		"proxyOf":              true,
		"needsSubStore":        true,
		"runProbe":             true,
	}
	for _, name := range coreFiles {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			for _, bad := range forbiddenImports {
				if strings.Contains(imp.Path.Value, bad) {
					t.Errorf("%s imports forbidden package %s", name, imp.Path.Value)
				}
			}
		}
		selected := map[*ast.Ident]bool{}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "convert" {
					switch sel.Sel.Name {
					case "ParseStructuredProxyString", "ConvertNodeToMihomo", "FixProxyFields", "ProxyStringConverter":
						t.Errorf("%s uses legacy convert API %s", name, sel.Sel.Name)
					}
				}
				selected[sel.Sel] = true
			}
			if field, ok := n.(*ast.Field); ok {
				for _, name := range field.Names {
					selected[name] = true
				}
			}
			if pair, ok := n.(*ast.KeyValueExpr); ok {
				if key, ok := pair.Key.(*ast.Ident); ok {
					selected[key] = true
				}
			}

			if iface, ok := n.(*ast.InterfaceType); ok && (iface.Methods == nil || len(iface.Methods.List) == 0) {
				t.Errorf("%s contains an empty interface{} literal", name)
			}
			return true
		})
		ast.Inspect(file, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && !selected[id] && forbiddenNames[id.Name] {
				t.Errorf("%s references forbidden identifier %s", name, id.Name)
			}
			return true
		})
	}
}

func TestHealthNativeOwnersHaveNoLegacyDependency(t *testing.T) {
	if _, err := os.Stat("legacy.go"); !os.IsNotExist(err) {
		t.Fatalf("legacy health facade remains: %v", err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), "owners.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range file.Imports {
		if strings.Contains(imp.Path.Value, "jsonx") || strings.Contains(imp.Path.Value, "pystr") {
			t.Errorf("native owner imports %s", imp.Path.Value)
		}
	}
}

func seedSharedRaw(t *testing.T, r *repo.Repository, rawJSON string) context.Context {
	t.Helper()
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	var patch model.Fields
	if err := json.Unmarshal([]byte(rawJSON), &patch); err != nil {
		t.Fatal(err)
	}
	// Imported malformed identities must be seeded as stored rows, not through a validating writer.
	db, err := sql.Open("sqlite", r.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for field, raw := range patch.All() {
		if _, err := db.ExecContext(ctx, "UPDATE fields SET value = ?, revision = revision + 1 WHERE scope = 'shared' AND owner = '' AND field = ?", string(raw), field); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, "UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'generation'"); err != nil {
		t.Fatal(err)
	}

	repo.ResetConfigContext(ctx)
	return ctx
}

// TestWrongTypedCandidateIdentitiesReadLeniently (O18 T1c; was
// TestWrongTypedCandidateIdentitiesFailWithOffendingValue): shared resources
// and cached nodes are plain records, so stored identities of the wrong type
// read with the stored-data rules (a number as its text, anything else as
// empty) instead of failing the whole latency run with Python's ValueError;
// the subscription cache is a data file and still fails with it.
func TestWrongTypedCandidateIdentitiesReadLeniently(t *testing.T) {
	tests := []struct {
		name      string
		sharedRaw string
		cacheRaw  string
		want      string
	}{
		{
			name:      "manual node numeric id",
			sharedRaw: `{"nodes":[{"id":7,"name":"A","type":"http","server":"a.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgStoredNodeIDInvalid, "7"),
		},
		{
			name:      "manual node array id",
			sharedRaw: `{"nodes":[{"id":["n1"],"name":"A","type":"http","server":"a.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgStoredNodeIDInvalid, `["n1"]`),
		},
		{
			name:      "manual node array name",
			sharedRaw: `{"nodes":[{"id":"n1","name":["A"],"type":"http","server":"a.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgStoredNodeNameInvalid, "n1", `["A"]`),
		},
		{
			name:      "manual node bool name",
			sharedRaw: `{"nodes":[{"id":"n1","name":true,"type":"http","server":"a.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgStoredNodeNameInvalid, "n1", "true"),
		},
		{
			name:      "subscription numeric id",
			sharedRaw: `{"subscriptions":[{"id":9,"name":"S","url":"https://sub.example.test/a"}]}`,
			want:      fmt.Sprintf(msgStoredSubIDInvalid, "9"),
		},
		{
			name:      "cached node array name",
			sharedRaw: `{"subscriptions":[{"id":"s1","name":"S","url":"https://sub.example.test/a"}]}`,
			cacheRaw:  `{"nodes":[{"name":["HK"],"type":"http","server":"hk.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgCachedNodeNameInvalid, "s1", `["HK"]`),
		},
		{
			name:      "cached node bool name",
			sharedRaw: `{"subscriptions":[{"id":"s1","name":"S","url":"https://sub.example.test/a"}]}`,
			cacheRaw:  `{"nodes":[{"name":false,"type":"http","server":"hk.example.test","port":80}]}`,
			want:      fmt.Sprintf(msgCachedNodeNameInvalid, "s1", "false"),
		},
		{
			name:      "cached nodes non-array",
			sharedRaw: `{"subscriptions":[{"id":"s1","name":"S","url":"https://sub.example.test/a"}]}`,
			cacheRaw:  `{"nodes":"bad"}`,
			want:      fmt.Sprintf(msgCachedNodesMustBeArray, "s1", `"bad"`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := seedSharedRaw(t, r, tt.sharedRaw)
			probeEnv := &ProbeEnv{
				LookupEnv:   func(string) (string, bool) { return "", false },
				Environ:     func() []string { return nil },
				Runner:      &fakeRunner{},
				Now:         time.Now,
				ProbeBinary: func() (string, error) { return "/test/probe", nil },
				Collaborators: Collaborators{
					LoadSubscriptionCache: func(context.Context, string) (json.RawMessage, error) {
						if tt.cacheRaw == "" {
							return nil, nil
						}
						return json.RawMessage(tt.cacheRaw), nil
					},
					FilterSubscriptionNodes: func(_ model.Subscription, nodes []json.RawMessage) ([]json.RawMessage, error) {
						return nodes, nil
					},
				},
			}
			restore := SetProbeEnv(probeEnv)
			defer restore()

			_, err = CollectCandidateNodes(ctx)
			if tt.cacheRaw != "" {
				// The subscription cache is a data file, checked as read.
				var ve *ValueError
				if !errors.As(err, &ve) || ve.Message != tt.want {
					t.Fatalf("CollectCandidateNodes: want ValueError %q, got %v", tt.want, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CollectCandidateNodes: Python's %q is no longer raised, got %v", tt.want, err)
			}
		})
	}
}

func TestNumericNodeNamesAcceptedLosslessly(t *testing.T) {
	const (
		manualBig = "9007199254740993"
		cachedBig = "9007199254740994"
	)
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := seedSharedRaw(t, r, `{
		"nodes":[{"id":"n1","name":`+manualBig+`,"type":"http","server":"a.example.test","port":80}],
		"subscriptions":[{"id":"s1","name":"Feed","url":"https://sub.example.test/a"}]
	}`)

	var storedFile json.RawMessage
	runner := &fakeRunner{
		fn: func(cmd Command) (Result, error) {
			var payload rawObject
			if err := json.Unmarshal(cmd.Stdin, &payload); err != nil {
				return Result{}, err
			}
			rawNodes, _ := payload.Fields.Get("nodes")
			var proxies []rawObject
			if err := json.Unmarshal(rawNodes, &proxies); err != nil {
				return Result{}, err
			}
			var results []map[string]any
			for _, p := range proxies {
				nameRaw, _ := p.Fields.Get("name")
				var name string
				_ = json.Unmarshal(nameRaw, &name)
				results = append(results, map[string]any{"name": name, "latency": 25})
			}
			stdout, _ := jsonresp.Encode(map[string]any{"version": 4, "results": results})
			return Result{Stdout: stdout}, nil
		},
	}

	collabs := defaultCollaborators(nil)
	collabs.LoadSubscriptionCache = func(context.Context, string) (json.RawMessage, error) {
		return json.RawMessage(`{"nodes":[{"name":` + cachedBig + `,"type":"http","server":"b.example.test","port":80}]}`), nil
	}
	collabs.ReadLatencyFile = func(context.Context) (json.RawMessage, bool) {
		return storedFile, len(storedFile) > 0
	}
	collabs.WriteLatencyFile = func(_ context.Context, data json.RawMessage) error {
		storedFile = append(json.RawMessage(nil), data...)
		return nil
	}

	restore := SetProbeEnv(&ProbeEnv{
		LookupEnv:     func(string) (string, bool) { return "", false },
		Environ:       func() []string { return nil },
		Runner:        runner,
		Now:           func() time.Time { return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) },
		ProbeBinary:   func() (string, error) { return "/test/probe", nil },
		Collaborators: collabs,
	})
	defer restore()

	candidates, err := CollectCandidateNodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || candidates[0].Name != manualBig || candidates[1].Name != cachedBig {
		t.Fatalf("unexpected candidates: %+v", candidates)
	}

	outcome, err := RunLatencyTests(ctx, []string{manualBig, cachedBig}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := outcome.Results.Get(manualBig); !ok {
		t.Fatalf("missing manual numeric node result: %+v", outcome)
	}
	if _, ok := outcome.Results.Get(cachedBig); !ok {
		t.Fatalf("missing cached numeric node result: %+v", outcome)
	}
}

func TestR5StoredFallbacksAndSystemPreservation(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := seedSharedRaw(t, r, `{"nodes":[{"id":"n1","name":"A","type":"http","server":"a.example.test","port":80}]}`)

	// Seed SystemConfig with an extra unknown member and verify SaveDNSConfigJSON preserves it.
	err = r.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.SystemConfig.ServerDomain = "panel.example.test"
		return markerSet(&sys.SystemConfig.GitHubProxyDomain, json.RawMessage(`"kept"`))
	})
	if err != nil {
		t.Fatal(err)
	}
	repo.ResetConfigContext(ctx)

	if _, err := SaveDNSConfigJSON(ctx, json.RawMessage(`{"mode":"custom","servers":["192.0.2.53"]}`)); err != nil {
		t.Fatal(err)
	}
	sysAfter, err := r.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	extra, hasExtra := markerGet(json.RawMessage(sysAfter.SystemConfig.GitHubProxyDomain))
	if sysAfter.SystemConfig.ServerDomain != "panel.example.test" || !hasExtra || string(extra) != `"kept"` {
		t.Fatalf("SaveDNSConfigJSON clobbered SystemConfig fields: domain=%q extra=%s", sysAfter.SystemConfig.ServerDomain, extra)
	}

	// Stored node_latency.json with non-list history falls back to empty history under R5 instead of iterating string chars.
	storedFile := json.RawMessage(`{"[\"node\",\"n1\"]":{"latency":10,"history":"corrupt","method":"proxy_http_v2","url":"https://www.gstatic.com/generate_204","unified_delay":true,"dns_servers":["192.0.2.53"],"dns_bootstrap":[]}}`)
	collabs := defaultCollaborators(nil)
	collabs.ReadLatencyFile = func(context.Context) (json.RawMessage, bool) { return storedFile, true }
	collabs.WriteLatencyFile = func(_ context.Context, data json.RawMessage) error {
		storedFile = append(json.RawMessage(nil), data...)
		return nil
	}
	restore := SetProbeEnv(&ProbeEnv{
		LookupEnv: func(string) (string, bool) { return "", false },
		Environ:   func() []string { return nil },
		Runner: &fakeRunner{
			result: Result{Stdout: []byte(`{"version":4,"results":[{"name":"[\"node\",\"n1\"]","latency":33}]}`)},
		},
		Now:           func() time.Time { return time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC) },
		ProbeBinary:   func() (string, error) { return "/test/probe", nil },
		Collaborators: collabs,
	})
	defer restore()

	outcome, err := RunLatencyTests(ctx, []string{"A"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := outcome.Results.Get("A")
	if !ok {
		t.Fatal("missing result A")
	}
	rawHistory, _ := json.Marshal(entry.History)
	if string(rawHistory) != "[33]" {
		t.Fatalf("expected R5 reset history [33], got %s", rawHistory)
	}
}
