package importer_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/importer"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

func TestSnellShadowTLSRoundTripThroughSurgeExporter(t *testing.T) {
	line := "Node = snell, example.com, 443, psk=p, version=5, reuse=true, shadow-tls-password=shadow, shadow-tls-sni=tls.example.com, shadow-tls-version=3"
	plan, err := importer.Parse("surge", "[Proxy]\n"+line+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Proxies) != 1 {
		t.Fatal("node missing")
	}
	proxy := plan.Proxies[0]
	fields := map[string]json.RawMessage{}
	for key, value := range proxy.Options {
		fields[key] = value
	}
	fields["name"], _ = json.Marshal(proxy.Name)
	fields["type"], _ = json.Marshal(proxy.Type)
	fields["server"] = proxy.Server
	fields["port"] = proxy.Port
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	proxyString := string(raw)
	actual, _, err := convert.SurgeNode(model.Node{Name: proxy.Name, ProxyString: &proxyString})
	if err != nil {
		t.Fatal(err)
	}
	if actual != line {
		t.Fatalf("round trip: %s", actual)
	}
}
func TestNativeImportsCommitThroughRepositoryAndPreserveExistingFields(t *testing.T) {
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 65536), 4<<20)
	for scan.Scan() {
		var row corpusRow
		if json.Unmarshal(scan.Bytes(), &row) != nil || !strings.HasPrefix(row.Name, "original-") {
			continue
		}
		t.Run(row.Client, func(t *testing.T) {
			r, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			rawShared, err := json.Marshal(row.Shared)
			if err != nil {
				t.Fatal(err)
			}
			var sharedMap map[string]any
			if err := json.Unmarshal(rawShared, &sharedMap); err != nil {
				t.Fatal(err)
			}
			if nodes, ok := sharedMap["nodes"].([]any); ok {
				for _, item := range nodes {
					item.(map[string]any)["future_field"] = map[string]any{"keep": true}
				}
			}
			shared := testjson.Decode[model.Shared](t, sharedMap)
			if _, err := r.SaveSharedRecord(ctx, &shared, nil); err != nil {
				t.Fatal(err)
			}
			plan, err := importer.Parse(row.Client, row.Content)
			if err != nil {
				t.Fatal(err)
			}
			counter := 0
			_, err = r.ImportClientProfileRecord(ctx, repo.ProfileCreation{ID: "imported", Name: "Imported"}, func(_ context.Context, current *model.Shared) (*repo.ClientProfileImport, error) {
				data, err := json.Marshal(current)
				if err != nil {
					return nil, err
				}
				var existing importer.Shared
				if err := json.Unmarshal(data, &existing); err != nil {
					return nil, err
				}
				built, err := importer.Build(plan, existing, importer.BuildOptions{DefaultSubscriptionIDs: row.Defaults, NewID: func(prefix string) (string, error) { counter++; return fmt.Sprintf("%s_%012x", prefix, counter), nil }})
				if err != nil {
					return nil, err
				}
				decoded := testjson.Decode[repo.ClientProfileImport](t, built)
				return &decoded, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			after, err := r.Shared(ctx)
			if err != nil {
				t.Fatal(err)
			}
			nodeRaw, err := json.Marshal(after.Nodes[0])
			if err != nil {
				t.Fatal(err)
			}
			parsedNode, err := testjson.Parse(nodeRaw)
			if err != nil {
				t.Fatal(err)
			}
			if keep, _ := testjson.Path(parsedNode, "future_field", "keep"); keep != true {
				t.Fatal("existing node fields lost")
			}
			profile, err := r.Profile(ctx, "imported")
			if err != nil {
				t.Fatal(err)
			}
			if len(profile.ProxyGroups) == 0 {
				t.Fatal("profile groups not committed")
			}
			before, err := r.ExportAllJSON(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.ImportClientProfileRecord(ctx, repo.ProfileCreation{ID: "broken", Name: "Broken"}, func(_ context.Context, _ *model.Shared) (*repo.ClientProfileImport, error) {
				decoded := testjson.Decode[repo.ClientProfileImport](t, map[string]any{
					"shared":  map[string]any{"nodes": []any{map[string]any{"id": "bad", "name": "bad", "type": "http", "enabled": true}}},
					"profile": map[string]any{"proxy_groups": []any{map[string]any{"id": "bad-group", "name": "DIRECT", "type": "select"}}},
				})
				return &decoded, nil
			})
			if err == nil {
				t.Fatal("invalid import committed")
			}
			afterDocument, err := r.ExportAllJSON(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			if !testjson.Equal(before, afterDocument) {
				t.Fatal("failed validation partially wrote resources")
			}
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
}
