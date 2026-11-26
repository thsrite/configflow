package oxidns

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"sort"
)

// SchemaFingerprint identifies the embedded validation schema bytes, independent
// of filesystem paths and JSON formatting by the encoder. It exposes no schemas.
func SchemaFingerprint() string {
	names, err := fs.Glob(schemaFS, "schemas/*.json")
	if err != nil {
		return ""
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		data, err := schemaFS.ReadFile(name)
		if err != nil {
			return ""
		}
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
