package convert_test

import (
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx/yamlxtest"
)

type mihomoReaderOracle struct{ references *jsonx.Object }

func loadMihomoReaderOracle(t testing.TB) mihomoReaderOracle {
	t.Helper()
	return mihomoReaderOracle{yamlxtest.Fixture(t, "testdata/n73d_expected.json.gz")}
}

func (o mihomoReaderOracle) row(t testing.TB, corpus string, index int, row mihomoRow) mihomoRow {
	t.Helper()
	reference := o.references.GetObject(fmt.Sprintf("%s:%d", corpus, index))
	if reference == nil {
		return row
	}
	if reference.GetString("fn") != row.fn || !jsonx.Equal(reference.Value("args"), row.entry.Value("args")) {
		t.Fatal("reader oracle source changed")
	}
	old := jsonx.NewObject()
	for _, key := range []string{"out", "err", "report", "mutated"} {
		if value, ok := row.entry.Get(key); ok {
			old.Set(key, value)
		}
	}
	if !jsonx.Equal(old, reference.Value("old")) || len(reference.GetList("classes")) == 0 {
		t.Fatal("reader oracle provenance/classification changed")
	}
	entry := row.entry.DeepCopy()
	for _, key := range []string{"out", "err", "report", "mutated", "document"} {
		entry.Delete(key)
	}
	for key, value := range reference.GetObject("expected").All() {
		entry.Set(key, value)
	}
	return rowOf(entry)
}

func TestN73DocumentReaderInjectionValues(t *testing.T) {
	sources := yamlxtest.Fixture(t, "testdata/n73d_reader_values.json.gz")
	if sources.Len() != 3837 {
		t.Fatalf("source count changed: %d", sources.Len())
	}
	for id, item := range sources.All() {
		record := item.(*jsonx.Object)
		if record.GetString("mode") != "convert-document" {
			t.Fatal("unexpected reader mode")
		}
		source := record.GetString("source")
		if fmt.Sprintf("%x", sha256.Sum256([]byte(record.GetString("mode")+"\x00"+source))) != id {
			t.Fatal("source digest changed")
		}
		root, err := yamlx.Parse([]byte(source))
		if err == nil {
			yamlxtest.CheckParseAudit(t, root, record.GetList("parse_nodes"))
			err = yamlx.ProjectJSONKeys(root, yamlx.RejectNonStringKeys)
		}
		if err != nil {
			if err.Error() != record.GetString("error") {
				t.Fatalf("refusal changed: %s", id)
			}
			continue
		}
		if record.GetString("error") != "" {
			t.Fatal("recorded refusal now accepted")
		}
		value, err := yamlbridge.Value(root)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(yamlxtest.ValueGraph(value))
		if err != nil {
			t.Fatal(err)
		}
		graph, err := jsonx.Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !jsonx.Equal(graph, record.Value("values")) {
			t.Fatalf("injected value graph changed: %s", id)
		}
	}
	refs := loadMihomoReaderOracle(t)
	if refs.references.Len() != 278 {
		t.Fatalf("expected changes no longer classified: %d", refs.references.Len())
	}
	counts := map[string]int{}
	for _, item := range refs.references.All() {
		row := item.(*jsonx.Object)
		if len(row.GetList("classes")) != 1 {
			t.Fatal("unclassified or ambiguous row")
		}
		counts[row.GetList("classes")[0].(string)]++
	}
	if !reflect.DeepEqual(counts, map[string]int{"YAML1.1/timestamp": 182, "native-diagnostics": 34, "nonfinite-scalar-text": 31, "tagged-collections": 31}) {
		t.Fatalf("classification drift: %v", counts)
	}
	t.Logf("sources=3837 expected changes=278 classified=278 classes=%v unknown=0", counts)
}
