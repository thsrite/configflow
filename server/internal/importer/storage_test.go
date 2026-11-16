package importer_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/importer"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func repositoryObject(t *testing.T, value any) *jsonx.Object {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.(*jsonx.Object)
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
			ctx := repo.WithRepository(context.Background(), r)
			shared := repositoryObject(t, row.Shared)
			for _, item := range shared.GetList("nodes") {
				item.(*jsonx.Object).Set("future_field", jsonx.Obj("keep", true))
			}
			if _, err := r.SaveShared(ctx, shared, nil); err != nil {
				t.Fatal(err)
			}
			plan, err := importer.Parse(row.Client, row.Content)
			if err != nil {
				t.Fatal(err)
			}
			counter := 0
			_, err = r.ImportClientProfile(ctx, jsonx.Obj("id", "imported", "name", "Imported"), func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
				data, err := jsonx.Compact(current)
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
				return repositoryObject(t, built), nil
			})
			if err != nil {
				t.Fatal(err)
			}
			after, err := r.GetShared(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !after.GetList("nodes")[0].(*jsonx.Object).GetObject("future_field").GetBool("keep") {
				t.Fatal("existing node fields lost")
			}
			profile, err := r.GetProfile(ctx, "imported")
			if err != nil {
				t.Fatal(err)
			}
			if len(profile.GetList("proxy_groups")) == 0 {
				t.Fatal("profile groups not committed")
			}
			before, err := r.ExportAll(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.ImportClientProfile(ctx, jsonx.Obj("id", "broken", "name", "Broken"), func(_ context.Context, _ *jsonx.Object) (*jsonx.Object, error) {
				return jsonx.Obj("shared", jsonx.Obj("nodes", []any{jsonx.Obj("id", "bad", "name", "bad", "type", "http", "enabled", true)}), "profile", jsonx.Obj("proxy_groups", []any{jsonx.Obj("id", "bad-group", "name", "DIRECT", "type", "select")})), nil
			})
			if err == nil {
				t.Fatal("invalid import committed")
			}
			afterDocument, err := r.ExportAll(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			if !jsonx.Equal(before, afterDocument) {
				t.Fatal("failed validation partially wrote resources")
			}
		})
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
}
