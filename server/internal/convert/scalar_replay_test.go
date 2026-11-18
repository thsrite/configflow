package convert

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

func nativeScalarReplay(t *testing.T, index int, row corpusRow, result any, err error) bool {
	t.Helper()
	if index != 2021 && index != 2103 {
		return false
	}
	data, readErr := os.ReadFile("testdata/n88_scalar_native.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	var fixtures []struct {
		Row    int    `json:"row"`
		Fn     string `json:"fn"`
		Input  string `json:"input_sha256"`
		Python string `json:"python_sha256"`
		Native string `json:"native_sha256"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil || len(fixtures) != 2 {
		t.Fatalf("native scalar fixtures: %v", err)
	}
	digest := func(value any) string {
		encoded, err := jsonx.Compact(value)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%x", sha256.Sum256(encoded))
	}
	for _, fixture := range fixtures {
		if fixture.Row != index {
			continue
		}
		if fixture.Fn != row.fn || digest(row.args) != fixture.Input || digest(row.out) != fixture.Python {
			t.Fatalf("native scalar row %d input/Python outcome changed", index)
		}
		if err != nil || digest(result) != fixture.Native {
			t.Fatalf("native scalar row %d differs: %v", index, err)
		}
		return true
	}
	return false
}

func TestGenericScalarReferenceRows(t *testing.T) {
	rows := loadCorpus(t)
	for _, index := range []int{2021, 2103} {
		result, _, err := runCorpusRow(rows[index])
		if !nativeScalarReplay(t, index, rows[index], result, err) {
			t.Fatalf("native scalar row %d was not checked", index)
		}
	}
}
