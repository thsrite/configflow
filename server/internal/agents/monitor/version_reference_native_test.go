package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"testing"

	"configflow/internal/jsonresp"
)

var versionRefOnce sync.Once
var versionRefIndex map[string]map[[32]byte]map[string]any
var versionRefErr error

func nativeVersionExpected(t *testing.T, section string, raw json.RawMessage) (map[string]any, bool) {
	t.Helper()
	versionRefOnce.Do(func() {
		data, err := os.ReadFile("testdata/n810_versions_native.json")
		if err != nil {
			versionRefErr = err
			return
		}
		var fixture struct {
			Sources map[string]string `json:"sources"`
			Rows    []struct {
				Source  string          `json:"source"`
				Section string          `json:"section"`
				SHA256  string          `json:"sha256"`
				Row     json.RawMessage `json:"row"`
				Want    json.RawMessage `json:"want"`
			} `json:"rows"`
		}
		if err := json.Unmarshal(data, &fixture); err != nil {
			versionRefErr = err
			return
		}
		for file, want := range fixture.Sources {
			data, err := os.ReadFile("testdata/" + file)
			if err != nil {
				versionRefErr = err
				return
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != want {
				versionRefErr = errStateSource
				return
			}
		}
		versionRefIndex = map[string]map[[32]byte]map[string]any{}
		for _, row := range fixture.Rows {
			if _, known := fixture.Sources[row.Source]; !known {
				versionRefErr = errStateSource
				return
			}
			encoded, err := jsonresp.Canonical(row.Row)
			if err != nil {
				versionRefErr = err
				return
			}
			sum := sha256.Sum256(encoded)
			if hex.EncodeToString(sum[:]) != row.SHA256 {
				versionRefErr = errStateSource
				return
			}
			value := parseJSONMap(t, row.Want)
			if versionRefIndex[row.Section] == nil {
				versionRefIndex[row.Section] = map[[32]byte]map[string]any{}
			}
			versionRefIndex[row.Section][sum] = value
		}
		if len(versionRefIndex["newer_release"]) != 4 || len(versionRefIndex["core_update"]) != 8 {
			versionRefErr = errStateSource
		}
	})
	if versionRefErr != nil {
		t.Fatal(versionRefErr)
	}
	encoded, err := jsonresp.Canonical(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, bound := versionRefIndex[section][sha256.Sum256(encoded)]
	return want, bound
}
