package importer_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/importer"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/testjson"
)

func TestImportedNamesCannotEscapeRepository(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "data")
	sentinel := filepath.Join(parent, "outside")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := repo.Open(dataDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	content := `proxies: [{name: '../../node', type: http, server: example.com, port: 80}]
proxy-groups: [{name: Pool, type: select, proxies: ['../../node']}]
rule-providers:
  '../../outside': {type: inline, behavior: domain, payload: [example.com]}
rules: ['RULE-SET,../../outside,Pool', 'MATCH,Pool']
`
	plan, err := importer.Parse("mihomo", content)
	if err != nil {
		t.Fatal(err)
	}
	build := func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
		result, err := importer.Build(plan, importer.Shared{}, importer.BuildOptions{})
		if err != nil {
			return nil, err
		}
		for _, item := range result.Shared.RuleLibrary {
			filename := rules.CacheFilename(testjson.Decode[model.RuleConfig](t, repositoryObject(t, item)))
			if !filepath.IsLocal(filename) {
				t.Fatal("imported rule name escapes cache directory")
			}
		}
		return repositoryObject(t, result), nil
	}
	_, err = r.ImportClientProfile(context.Background(), jsonx.Obj("id", "safe", "name", "Imported"), build)
	if err != nil {
		t.Fatal(err)
	}
	before, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"../outside", "..", "/absolute", "a/b", "a\\b"} {
		if _, err := r.ImportClientProfile(context.Background(), jsonx.Obj("id", id, "name", "Unsafe"), build); err == nil {
			t.Fatalf("unsafe profile ID accepted: %q", id)
		}
	}
	after, err := r.Store().Fingerprint()
	if err != nil || before != after {
		t.Fatal("rejected path changed database")
	}
	bytes, err := os.ReadFile(sentinel)
	if err != nil || string(bytes) != "unchanged" {
		t.Fatal("import changed external file")
	}
	err = filepath.WalkDir(parent, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path != parent && path != sentinel && path != dataDir && !strings.HasPrefix(path, dataDir+string(os.PathSeparator)) {
			t.Errorf("unexpected file outside repository: %s", filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
