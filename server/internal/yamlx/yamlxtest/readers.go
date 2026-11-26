package yamlxtest

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func mapString(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func mapBool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func mapList(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

func mapObject(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func mapHas(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func mapInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// Fixture reads a gzip JSON-object fixture without normalizing its values.
func Fixture(t testing.TB, path string) map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	data, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

// ReaderCases reads the separately recorded Python business expectations.
func ReaderCases(t testing.TB, path string) map[string]any {
	t.Helper()
	return mapObject(Fixture(t, path), "cases")
}

// CheckReaderFixtures checks every recorded actual loader input against the old
// API selected for its caller and against Parse plus the actual production key
// projector. It verifies the unmodified physical tag/alias audit before applying
// projection, and requires every changed Python expectation to have a load class.
// projectors are transition functions only, never consumer business functions.
func CheckReaderFixtures(t testing.TB, valuesPath, expectedPath string, projectors map[string]func(*yamlx.Node) error) {
	t.Helper()
	file, err := os.Open(valuesPath)
	if err != nil {
		t.Fatal(err)
	}
	historyErr := historicalFixture(file)
	file.Close()
	if historyErr != nil {
		t.Fatal(historyErr)
	}
	sources := Fixture(t, valuesPath)
	expected := Fixture(t, expectedPath)
	modes, classes := map[string]int{}, map[string]int{}
	for id, item := range sources {
		record := item.(map[string]any)
		source, mode := mapString(record, "source"), mapString(record, "mode")
		digest := sha256.Sum256([]byte(mode + "\x00" + source))
		if hex.EncodeToString(digest[:]) != id {
			t.Fatalf("bad fixture input hash %s", id)
		}
		modes[mode]++
		for _, category := range mapList(record, "classes") {
			label := category.(string)
			if label == "UNCLASSIFIED" {
				t.Fatalf("unclassified %s", id)
			}
			classes[label]++
		}
		history, err := HistoricalReader(mode, source)
		if err != nil {
			t.Fatal(err)
		}
		if history.OldError != mapString(record, "old_error") {
			t.Fatalf("historical loader error changed for %s", id)
		}
		n, err := yamlx.Parse([]byte(source))
		if err == nil {
			CheckParseAudit(t, n, mapList(record, "parse_nodes"))
			project, ok := projectors[mode]
			if !ok {
				t.Fatalf("unknown reader mode %s", mode)
			}
			err = project(n)
		}
		newMessage := ""
		if err != nil {
			newMessage = err.Error()
		}
		if newMessage != mapString(record, "error") {
			t.Fatalf("native load changed %s: got %v, recorded %s", id, err, mapString(record, "error"))
		}
		if err != nil && !(errors.Is(err, yamlx.ErrSyntax) || errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrType) || errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrAliasRecursion)) {
			t.Fatalf("unclassified error %s: %v", id, err)
		}
	}
	totals, changed, classified := map[string]int{}, map[string]int{}, map[string]int{}
	for id, item := range mapObject(expected, "cases") {
		row := item.(map[string]any)
		domain, _, _ := strings.Cut(id, "/")
		totals[domain]++
		if mapBool(row, "unchanged_original") {
			if domain != "convert" || mapBool(row, "changed") || mapHas(row, "old") || mapHas(row, "expected") {
				t.Fatalf("invalid original-golden manifest %s", id)
			}
			// collect checked the full original Python result against its old
			// golden. Unchanged convert rows use that golden directly, not a
			// second order-normalised copy in this large fixture.
			continue
		}
		actualChange := !EqualJSON(row["old"], row["expected"])
		if actualChange != mapBool(row, "changed") {
			t.Fatalf("false changed flag %s", id)
		}
		if !actualChange {
			continue
		}
		changed[domain]++
		if len(mapList(row, "classes")) == 0 {
			t.Fatalf("unexplained expected change %s", id)
		}
		for _, key := range append(append([]any{}, mapList(row, "old_loads")...), mapList(row, "native_loads")...) {
			if mapObject(sources, key.(string)) == nil {
				t.Fatalf("missing loader input %s", id)
			}
		}
		classified[domain]++
	}
	for domain, count := range changed {
		if count != classified[domain] {
			t.Fatalf("%s expected=%d classified=%d", domain, count, classified[domain])
		}
	}
	t.Logf("loader modes=%v classes=%v", modes, classes)
	t.Logf("business rows=%v expected changes=%v classified=%v unknown=0", totals, changed, classified)
}

// CheckParseAudit verifies the original physical tags, positions and sharing.
func CheckParseAudit(t testing.TB, root *yamlx.Node, records []any) {
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
		row := records[id-1].(map[string]any)
		if mapInt(row, "id") != id || mapInt(row, "kind") != int(n.Kind) || mapString(row, "tag") != n.Tag || mapString(row, "value") != n.Value || mapInt(row, "Line") != n.Line || mapInt(row, "Column") != n.Column {
			t.Fatalf("raw Parse audit changed at node %d", id)
		}
		children := mapList(row, "content")
		if len(children) != len(n.Content) {
			t.Fatal("audit child count mismatch")
		}
		for i, child := range n.Content {
			value, _ := children[i].(json.Number).Int64()
			walk(child, int(value))
		}
		walk(n.Alias, mapInt(row, "alias"))
	}
	walk(root, 1)
	if len(seen) != len(records) {
		t.Fatal("audit includes unreachable nodes")
	}
}
