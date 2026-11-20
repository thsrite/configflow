package convert_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

type mihomoReaderOracle struct{ references *convert.JObj }

func loadMihomoReaderOracle(t testing.TB) mihomoReaderOracle {
	t.Helper()
	return mihomoReaderOracle{loadGzipJObj(t, "testdata/n73d_expected.json.gz")}
}

func (o mihomoReaderOracle) row(t testing.TB, corpus string, index int, row mihomoRow) mihomoRow {
	t.Helper()
	reference := o.references.GetObject(fmt.Sprintf("%s:%d", corpus, index))
	if reference == nil {
		return row
	}
	if reference.GetString("fn") != row.fn || !convert.EqualJ(reference.Value("args"), row.entry.Value("args")) {
		t.Fatal("reader oracle source changed")
	}
	old := convert.NewJObj()
	for _, key := range []string{"out", "err", "report", "mutated"} {
		if value, ok := row.entry.Get(key); ok {
			old.Set(key, value)
		}
	}
	if !convert.EqualJ(old, reference.Value("old")) || len(reference.GetList("classes")) == 0 {
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

func checkParseAuditJ(t testing.TB, root *yamlx.Node, records []any) {
	t.Helper()
	seen := map[*yamlx.Node]int{}
	var walk func(*yamlx.Node, int)
	walk = func(n *yamlx.Node, id int) {
		if n == nil {
			if id != 0 {
				t.Fatal("audit alias target mismatch")
			}
			return
		}
		if id < 1 || id > len(records) {
			t.Fatal("audit node missing")
		}
		if previous := seen[n]; previous != 0 {
			if previous != id {
				t.Fatal("audit sharing mismatch")
			}
			return
		}
		seen[n] = id
		row := records[id-1].(*convert.JObj)
		if int(row.GetInt("id")) != id || int(row.GetInt("kind")) != int(n.Kind) || row.GetString("tag") != n.Tag || row.GetString("value") != n.Value || int(row.GetInt("Line")) != n.Line || int(row.GetInt("Column")) != n.Column {
			t.Fatalf("raw Parse audit changed at node %d", id)
		}
		children := row.GetList("content")
		if len(children) != len(n.Content) {
			t.Fatal("audit child count mismatch")
		}
		for i, child := range n.Content {
			value, _ := children[i].(json.Number).Int64()
			walk(child, int(value))
		}
		walk(n.Alias, int(row.GetInt("alias")))
	}
	walk(root, 1)
	if len(seen) != len(records) {
		t.Fatal("audit includes unreachable nodes")
	}
}

func TestN73DocumentReaderInjectionValues(t *testing.T) {
	sources := loadGzipJObj(t, "testdata/n73d_reader_values.json.gz")
	if sources.Len() != 3837 {
		t.Fatalf("source count changed: %d", sources.Len())
	}
	for id, item := range sources.All() {
		record := item.(*convert.JObj)
		if record.GetString("mode") != "convert-document" {
			t.Fatal("unexpected reader mode")
		}
		source := record.GetString("source")
		if fmt.Sprintf("%x", sha256.Sum256([]byte(record.GetString("mode")+"\x00"+source))) != id {
			t.Fatal("source digest changed")
		}
		root, err := yamlx.Parse([]byte(source))
		if err == nil {
			checkParseAuditJ(t, root, record.GetList("parse_nodes"))
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
		graph, err := convert.ParseJ(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if !convert.EqualJ(graph, record.Value("values")) {
			t.Fatalf("injected value graph changed: %s", id)
		}
	}
	refs := loadMihomoReaderOracle(t)
	if refs.references.Len() != 278 {
		t.Fatalf("expected changes no longer classified: %d", refs.references.Len())
	}
	counts := map[string]int{}
	for _, item := range refs.references.All() {
		row := item.(*convert.JObj)
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
