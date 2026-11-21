package repo

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/store"
)

// D1 for stored JSON: a row whose value repeats a member name is refused at
// load like any corrupt row. The diagnostic locates the row, keeps the
// recovery advice, never echoes stored values, and leaves the file untouched.
func TestN83ReviewDuplicateMemberRowIsLocatedWithoutValues(t *testing.T) {
	const secret = "synthetic-secret-never-in-model-diagnostics"
	r := newRepo(t)
	value := `{"password":"` + secret + `","nested":{"k":1,"k":2}}`
	rcSQL(t, r.DatabasePath(), `INSERT INTO fields(scope,owner,field,position,value,revision) VALUES('profile','default','future_dup',999,CAST(X'`+hex.EncodeToString([]byte(value))+`' AS TEXT),1)`)
	before := must(os.ReadFile(r.DatabasePath()))(t)
	_, err := OpenDefault(r.DataDir())
	if err == nil {
		t.Fatal("duplicate member row loaded")
	}
	message := err.Error()
	for _, part := range []string{r.DatabasePath(), "profile/default/future_dup", "成员名重复", "请先停止服务并保留原文件", "从可用备份恢复"} {
		if !strings.Contains(message, part) {
			t.Fatalf("diagnostic omitted %q", part)
		}
	}
	if strings.Contains(message, secret) || strings.Contains(message, "password") {
		t.Fatal("diagnostic exposed stored contents")
	}
	if !bytes.Equal(before, must(os.ReadFile(r.DatabasePath()))(t)) {
		t.Fatal("failed load changed database bytes")
	}
}

// clone_profile through the typed skeleton: copying a loaded profile and
// giving it a new ID writes a new section; the source rows are not rewritten.
func TestN83ReviewCopiedProfileWritesOnlyNewRows(t *testing.T) {
	original := modelRowFixture(t)
	document, _, err := decodeModelRows(original)
	noError(t, err)
	clone := *document.Profiles[0]
	clone.ID = "copy-of-" + document.Profiles[0].ID
	document.Profiles = append(document.Profiles, &clone)
	encoded := must(encodeModelRows(document, original))(t)
	if len(encoded.Profiles) != len(original.Profiles)+1 {
		t.Fatalf("profiles=%d", len(encoded.Profiles))
	}
	copied := encoded.Profiles[len(encoded.Profiles)-1]
	if copied.ID != clone.ID {
		t.Fatal("copy kept the source section key")
	}
	trimmed := *encoded
	trimmed.Profiles = encoded.Profiles[:len(original.Profiles)]
	assertModelRows(t, &trimmed, original)
	var id string
	noError(t, json.Unmarshal(copied.Fields[0].Value, &id))
	if copied.Fields[0].Name != "id" || id != clone.ID {
		t.Fatal("copied id row")
	}
	target := store.New(t.TempDir()+"/"+store.Filename, LockTimeout)
	base := must(target.Create(original, nil))(t)
	if _, changed, err := target.Commit(base, encoded, nil); err != nil || !changed {
		t.Fatalf("store refused the copy: %v %v", changed, err)
	}
}
