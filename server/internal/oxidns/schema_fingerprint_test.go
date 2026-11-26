package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
	"testing"
)

func TestSchemaFingerprintCoversEmbeddedFiles(t *testing.T) {
	t.Parallel()
	names, err := fs.Glob(schemaFS, "schemas/*.json")
	if err != nil || len(names) == 0 {
		t.Fatal(names, err)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		data, err := schemaFS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	if got := SchemaFingerprint(); got != hex.EncodeToString(h.Sum(nil)) {
		t.Fatal("schema fingerprint changed or omitted an embedded input")
	}
}
