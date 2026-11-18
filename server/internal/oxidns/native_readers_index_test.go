package oxidns

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// These source fingerprints register the N8-9 reader transition separately from
// the frozen N7 index. Admission also requires complete shared-facade equality.
//
//go:embed testdata/native-reader-expectations.json
var nativeReaderIndexData []byte
var nativeReaderIndex = sync.OnceValues(func() (map[string]string, error) {
	var values map[string]string
	err := json.Unmarshal(nativeReaderIndexData, &values)
	return values, err
})

func registeredNativeReaderSource(source string) (string, error) {
	sum := sha256.Sum256([]byte(source))
	key := hex.EncodeToString(sum[:])
	index, err := nativeReaderIndex()
	if err != nil {
		return "", err
	}
	if index[key] != "shared-native-graph" {
		return "", fmt.Errorf("unregistered N8-9 native reader %s", key)
	}
	return "N8-9-native-schema", nil
}
func TestNativeReaderIndexRejectsUnregisteredInput(t *testing.T) {
	if _, err := registeredNativeReaderSource("new_unregistered_field: 0123\n"); err == nil {
		t.Fatal("unknown reader transition was waived")
	}
}
