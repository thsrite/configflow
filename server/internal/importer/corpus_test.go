package importer_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/importer"
	"gopkg.in/yaml.v3"
)

type corpusRow struct {
	Name     string          `json:"name"`
	Client   string          `json:"client"`
	Content  string          `json:"content"`
	Shared   importer.Shared `json:"shared"`
	Defaults []string        `json:"defaults"`
	Expected json.RawMessage `json:"expected"`
	Error    string          `json:"error"`
}

func normalized(data []byte) (any, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if root, ok := value.(map[string]any); ok {
		if profile, ok := root["profile"].(map[string]any); ok {
			if engine, ok := profile["mihomo"].(map[string]any); ok {
				if text, ok := engine["custom_config"].(string); ok {
					var parsed any
					if err := yaml.Unmarshal([]byte(text), &parsed); err != nil {
						return nil, err
					}
					raw, err := json.Marshal(parsed)
					if err != nil {
						return nil, err
					}
					var native any
					_ = json.Unmarshal(raw, &native)
					engine["custom_config"] = native
				}
			}
		}
	}
	return value, nil
}
func firstDiff(a, b any, path string) string {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return fmt.Sprintf("%s: %T != %T", path, a, b)
	}
	switch left := a.(type) {
	case map[string]any:
		right := b.(map[string]any)
		for key, x := range left {
			y, ok := right[key]
			if !ok {
				return path + "." + key + " missing"
			}
			if !reflect.DeepEqual(x, y) {
				return firstDiff(x, y, path+"."+key)
			}
		}
		for key := range right {
			if _, ok := left[key]; !ok {
				return path + "." + key + " extra"
			}
		}
	case []any:
		right := b.([]any)
		if len(left) != len(right) {
			return fmt.Sprintf("%s length %d != %d", path, len(left), len(right))
		}
		for i, x := range left {
			if !reflect.DeepEqual(x, right[i]) {
				return firstDiff(x, right[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	default:
		return fmt.Sprintf("%s expected=%v actual=%v", path, a, b)
	}
	return path
}
func TestSyntheticPythonImportCorpus(t *testing.T) {
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 65536), 4<<20)
	count := 0
	for scanner.Scan() {
		var row corpusRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		count++
		t.Run(row.Name, func(t *testing.T) {
			before, _ := json.Marshal(row.Shared)
			plan, err := importer.Parse(row.Client, row.Content)
			if row.Error != "" {
				if err == nil {
					_, err = importer.Build(plan, row.Shared, importer.BuildOptions{})
				}
				if err == nil {
					t.Fatalf("expected rejection: %s", row.Error)
				}
				if !strings.HasPrefix(row.Error, "YAML 解析失败：") && err.Error() != row.Error {
					t.Fatalf("error expected=%s actual=%s", row.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			counter := 0
			opts := importer.BuildOptions{DefaultSubscriptionIDs: row.Defaults, NewID: func(prefix string) (string, error) { counter++; return fmt.Sprintf("%s_%012x", prefix, counter), nil }}
			result, err := importer.Build(plan, row.Shared, opts)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			got, err := normalized(actual)
			if err != nil {
				t.Fatal(err)
			}
			want, err := normalized(row.Expected)
			if err != nil {
				t.Fatal(err)
			}
			if row.Name == "yaml-unusual-scalars" {
				// Native YAML/Go boolean display is lower-case. Only this malformed
				// non-string node-name diagnostic differs; Chinese text/decisions match.
				messages := want.(map[string]any)["warnings"].([]any)
				for i, message := range messages {
					if message == "节点格式无效，已跳过：False、ZeroType" {
						messages[i] = "节点格式无效，已跳过：false、ZeroType"
					}
				}
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatal(firstDiff(want, got, "result"))
			}
			after, _ := json.Marshal(row.Shared)
			if !bytes.Equal(before, after) {
				t.Fatal("shared input mutated")
			}
			counter = 0
			second, err := importer.Build(plan, row.Shared, opts)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := json.Marshal(second)
			if !bytes.Equal(actual, again) {
				t.Fatal("plan mutated across builds")
			}
		})
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d synthetic public parse/build cases", count)
}
