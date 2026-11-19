package monitor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
)

var stateRefOnce sync.Once
var stateRefIndex map[string]map[[32]byte]string
var stateRefErr error

func nativeStateExpectation(t *testing.T, section string, row any) (string, bool) {
	t.Helper()
	stateRefOnce.Do(func() {
		data, err := os.ReadFile("testdata/n810_state_native.json")
		if err != nil {
			stateRefErr = err
			return
		}
		var fixture struct {
			Sources map[string]string `json:"sources"`
			Rows    []struct {
				Source   string          `json:"source"`
				Section  string          `json:"section"`
				SHA256   string          `json:"sha256"`
				Row      json.RawMessage `json:"row"`
				Want     json.RawMessage `json:"want"`
				WantText string          `json:"want_text"`
			} `json:"rows"`
		}
		if err = json.Unmarshal(data, &fixture); err != nil {
			stateRefErr = err
			return
		}
		for file, want := range fixture.Sources {
			data, err := os.ReadFile("testdata/" + file)
			if err != nil {
				stateRefErr = err
				return
			}
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) != want {
				stateRefErr = errStateSource
				return
			}
		}
		stateRefIndex = map[string]map[[32]byte]string{}
		for _, row := range fixture.Rows {
			if _, known := fixture.Sources[row.Source]; !known {
				stateRefErr = errStateSource
				return
			}
			raw, err := jsonresp.Canonical(row.Row)
			if err != nil {
				stateRefErr = err
				return
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != row.SHA256 {
				stateRefErr = fmt.Errorf("%w: %s %s row hash got %s want %s", errStateSource, row.Source, row.Section, hex.EncodeToString(sum[:]), row.SHA256)
				return
			}
			want := row.Want
			if row.WantText != "" {
				want = []byte(row.WantText)
			}
			value, err := jsonx.Parse(want)
			if err != nil {
				stateRefErr = err
				return
			}
			if stateRefIndex[row.Section] == nil {
				stateRefIndex[row.Section] = map[[32]byte]string{}
			}
			stateRefIndex[row.Section][sum] = dumpSorted(t, value)
		}
		if len(stateRefIndex["advance"]) != 88 || len(stateRefIndex["state_files"]) != 3 || len(stateRefIndex["responses"]) != 4 || len(stateRefIndex["metrics"]) != 15 || len(stateRefIndex["target"]) != 17 || len(stateRefIndex["overview_snapshot"]) != 1 {
			stateRefErr = fmt.Errorf("%w: counts advance=%d state=%d response=%d metric=%d target=%d", errStateSource, len(stateRefIndex["advance"]), len(stateRefIndex["state_files"]), len(stateRefIndex["responses"]), len(stateRefIndex["metrics"]), len(stateRefIndex["target"]))
		}
	})
	if stateRefErr != nil {
		t.Fatal(stateRefErr)
	}
	raw, err := jsonx.Compact(row)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = jsonresp.Canonical(raw)
	if err != nil {
		t.Fatal(err)
	}
	want, exists := stateRefIndex[section][sha256.Sum256(raw)]
	return want, exists
}

var errStateSource = &StateError{"native state reference source/binding changed"}

func TestNativeStateReferenceRejectsUnboundRow(t *testing.T) {
	ref := loadReference(t, "advance")
	matched := 0
	for _, c := range cases(ref, "advance") {
		if _, ok := nativeStateExpectation(t, "advance", c); !ok {
			continue
		}
		matched++
		changed := c.DeepCopy()
		changed.Set("target", "unbound-target")
		if _, ok := nativeStateExpectation(t, "advance", changed); ok {
			t.Fatal("changed input matched native exception")
		}
	}
	if matched != 88 {
		t.Fatalf("matched %d reviewed rows", matched)
	}
}
