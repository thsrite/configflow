package convert_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/convert"
)

type subsCallerCase struct {
	InputSHA      string          `json:"input_sha256"`
	PythonError   [2]string       `json:"python_error"`
	NativeError   [2]string       `json:"native_error"`
	Reason        string          `json:"reason"`
	NativeMutated json.RawMessage `json:"native_mutated"`
}

func nativeSubsCallers(t *testing.T, archive string) map[int]subsCallerCase {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_subs_callers_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Sources map[string]string         `json:"sources"`
		Cases   map[string]subsCallerCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/" + archive + ".jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != fixture.Sources[archive] {
		t.Fatal("subscription caller source drift")
	}
	reader, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 1<<24)
	result := map[int]subsCallerCase{}
	for index := 0; scanner.Scan(); index++ {
		expected, ok := fixture.Cases[archive+"/"+strconv.Itoa(index)]
		if !ok {
			continue
		}
		sum := sha256.Sum256(scanner.Bytes())
		if hex.EncodeToString(sum[:]) != expected.InputSHA || expected.Reason == "" {
			t.Fatal("subscription caller input drift", index)
		}
		var original struct {
			Error [2]string `json:"err"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &original); err != nil || original.Error != expected.PythonError {
			t.Fatal("historical caller error changed", index)
		}
		result[index] = expected
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for key := range fixture.Cases {
		if strings.HasPrefix(key, archive+"/") {
			count++
		}
	}
	if len(result) != count {
		t.Fatal("unconsumed caller fixture")
	}
	return result
}

func TestN89CallerReferenceProvenance(t *testing.T) {
	if len(nativeSubsCallers(t, "surge_loon_corpus")) != 22 {
		t.Fatal("caller reference count changed")
	}
	for _, archive := range []string{"review_c12", "corpus"} {
		versions := nativeSubsCallers(t, archive)
		if len(versions) != 2 {
			t.Fatal("version reference count changed")
		}
		rows := loadJSONLines(t, "testdata/"+archive+".jsonl.gz")
		for index, expected := range versions {
			args := rows[index].GetList("args")
			docNode, err := convert.YAMLNodeFromJ(args[0])
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := convert.DecodeConfigJ(args[1].(*convert.JObj))
			if err != nil {
				t.Fatal(err)
			}
			version, err := coreVersionRaw(args[3])
			if err != nil {
				t.Fatal(err)
			}
			_, err = convert.ApplyEasyTierNode(docNode, cfg, true, version)
			gotDoc, convErr := convert.JFromYAMLNode(docNode)
			mutated, parseErr := convert.ParseJ(expected.NativeMutated)
			if expected.NativeError[0] != "EasyTierSettingsError" || err == nil || err.Error() != expected.NativeError[1] || convErr != nil || parseErr != nil || !convert.EqualJ(gotDoc, mutated) {
				t.Fatalf("Agent fixture %s/%d mismatch: %v", archive, index, err)
			}
		}
	}
}
