package mosdns

import (
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/yamlx/yamltest"
)

func TestN73MosDNSReaderMigrationGate(t *testing.T) {
	for _, path := range []string{"testdata/corpus.jsonl.gz", "testdata/recorded.jsonl.gz"} {
		byCaller := map[string][]string{}
		for _, row := range loadRows(t, path) {
			args := row.GetList("args")
			if len(args) == 0 {
				continue
			}
			switch row.GetString("fn") {
			case "generate":
				byCaller["custom/upstreams"] = append(byCaller["custom/upstreams"], yamltest.TextFields(args[0], "custom_config", "local_dns", "remote_dns", "fallback_dns")...)
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
			yamltest.Check(t, path+"/"+caller, sources)
		}
	}
}

func TestN73ArchiveAndReviewReaderMigrationGate(t *testing.T) {
	var sources []string
	for _, row := range loadRows(t, "testdata/c28_recorded.jsonl.gz") {
		if row.GetString("fn") == "build" {
			args := row.GetList("args")
			if len(args) > 1 {
				if text, ok := args[1].(string); ok {
					sources = append(sources, text)
				}
			}
		}
	}
	yamltest.Check(t, "mosdns/archive.checkReferences", sources)
	data, err := os.ReadFile("testdata/review_c21_ref.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	sources = nil
	for key := range value.(*jsonx.Object).All() {
		kind, source, _ := strings.Cut(key, ":")
		if kind == "convert" {
			_, source, _ = strings.Cut(source, ":")
		}
		sources = append(sources, source)
	}
	yamltest.Check(t, "mosdns/C21-review", sources)
}
