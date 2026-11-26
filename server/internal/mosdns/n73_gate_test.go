package mosdns

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"configflow/internal/rules"
	"configflow/internal/yamlx/yamlxtest"
)

func TestN73MosDNSReaderMigrationGate(t *testing.T) {
	for _, path := range []string{"testdata/corpus.jsonl.gz", "testdata/recorded.jsonl.gz"} {
		byCaller := map[string][]string{}
		for _, row := range loadArchiveRows(t, path) {
			args, _ := row["args"].([]any)
			if len(args) == 0 {
				continue
			}
			switch mapStr(row, "fn") {
			case "generate":
				byCaller["custom/upstreams"] = append(byCaller["custom/upstreams"], yamlxtest.TextFields(args[0], "custom_config", "local_dns", "remote_dns", "fallback_dns")...)
			case "upstreams":
				if source, ok := args[0].(string); ok {
					byCaller["upstreams"] = append(byCaller["upstreams"], source)
				}
			case "convert":
				if source, ok := args[0].(string); ok && rules.HasYAMLPayload(source) {
					byCaller["payload"] = append(byCaller["payload"], source)
				}
			}
		}
		for caller, sources := range byCaller {
			yamlxtest.Check(t, path+"/"+caller, sources)
		}
	}
}

func TestN73ArchiveAndReviewReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range loadArchiveRows(t, "testdata/c28_recorded.jsonl.gz") {
		if mapStr(row, "fn") == "build" {
			args, _ := row["args"].([]any)
			if len(args) > 1 {
				if text, ok := args[1].(string); ok {
					sources = append(sources, text)
				}
			}
		}
	}
	yamlxtest.Check(t, "mosdns/archive.checkReferences", sources)
	data, err := os.ReadFile("testdata/review_c21_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	sources = nil
	for key := range value {
		kind, source, _ := strings.Cut(key, ":")
		if kind == "convert" {
			_, source, _ = strings.Cut(source, ":")
		}
		sources = append(sources, source)
	}
	yamlxtest.Check(t, "mosdns/C21-review", sources)
}
