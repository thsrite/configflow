package yamlxtest

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Fixture reads a gzip JSON-object fixture without normalizing its values.
func Fixture(t testing.TB, path string) *jsonx.Object {
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
	v, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*jsonx.Object)
}

// ReaderCases reads the separately recorded Python business expectations.
func ReaderCases(t testing.TB, path string) *jsonx.Object {
	t.Helper()
	return Fixture(t, path).GetObject("cases")
}

// CheckReaderFixtures checks every recorded actual loader input against the old
// API selected for its caller and against Parse plus the actual production key
// projector. It verifies the unmodified physical tag/alias audit before applying
// projection, and requires every changed Python expectation to have a load class.
// projectors are transition functions only, never consumer business functions.
func CheckReaderFixtures(t testing.TB, valuesPath, expectedPath string, projectors map[string]func(*yamlx.Node) error) {
	t.Helper()
	sources := Fixture(t, valuesPath)
	expected := Fixture(t, expectedPath)
	modes, classes := map[string]int{}, map[string]int{}
	for id, item := range sources.All() {
		record := item.(*jsonx.Object)
		source, mode := record.GetString("source"), record.GetString("mode")
		digest := sha256.Sum256([]byte(mode + "\x00" + source))
		if hex.EncodeToString(digest[:]) != id {
			t.Fatalf("bad fixture input hash %s", id)
		}
		modes[mode]++
		for _, category := range record.GetList("classes") {
			label := category.(string)
			if label == "UNCLASSIFIED" {
				t.Fatalf("unclassified %s", id)
			}
			classes[label]++
		}
		var oldErr error
		// This is the dedicated migration gate. Consumers never call these old APIs.
		switch mode {
		case "parser", "core-corpus":
			_, oldErr = yamlx.SafeLoad(source)
		case "dialers":
			_, oldErr = yamlx.LoadCSafeFirst(source, yamlx.LoadOptions{KeepNonStringKeys: true})
		case "gateway-payload":
			_, oldErr = yamlx.LoadWith(source, yamlx.LoadOptions{KeepNonStringKeys: true, KeepUnrepresentable: true})
		default:
			_, oldErr = yamlx.LoadWith(source, yamlx.LoadOptions{KeepNonStringKeys: true})
		}
		oldMessage := ""
		if oldErr != nil {
			oldMessage = oldErr.Error()
		}
		if oldMessage != record.GetString("old_error") {
			t.Fatalf("old API changed for %s", id)
		}
		n, err := yamlx.Parse([]byte(source))
		if err == nil {
			CheckParseAudit(t, n, record.GetList("parse_nodes"))
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
		if newMessage != record.GetString("error") {
			t.Fatalf("native load changed %s: got %v, recorded %s", id, err, record.GetString("error"))
		}
		if err != nil && !(errors.Is(err, yamlx.ErrSyntax) || errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrType) || errors.Is(err, yamlx.ErrAliasBudget) || errors.Is(err, yamlx.ErrAliasRecursion)) {
			t.Fatalf("unclassified error %s: %v", id, err)
		}
	}
	totals, changed, classified := map[string]int{}, map[string]int{}, map[string]int{}
	for id, item := range expected.GetObject("cases").All() {
		row := item.(*jsonx.Object)
		domain, _, _ := strings.Cut(id, "/")
		totals[domain]++
		if row.GetBool("unchanged_original") {
			if domain != "convert" || row.GetBool("changed") || row.Has("old") || row.Has("expected") {
				t.Fatalf("invalid original-golden manifest %s", id)
			}
			// collect checked the full original Python result against its old
			// golden. Unchanged convert rows use that golden directly, not a
			// second order-normalised copy in this large fixture.
			continue
		}
		actualChange := !EqualJSON(row.Value("old"), row.Value("expected"))
		if actualChange != row.GetBool("changed") {
			t.Fatalf("false changed flag %s", id)
		}
		if !actualChange {
			continue
		}
		changed[domain]++
		if len(row.GetList("classes")) == 0 {
			t.Fatalf("unexplained expected change %s", id)
		}
		for _, key := range append(append([]any{}, row.GetList("old_loads")...), row.GetList("native_loads")...) {
			if sources.GetObject(key.(string)) == nil {
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
		row := records[id-1].(*jsonx.Object)
		if int(row.GetInt("id")) != id || int(row.GetInt("kind")) != int(n.Kind) || row.GetString("tag") != n.Tag || row.GetString("value") != n.Value || int(row.GetInt("Line")) != n.Line || int(row.GetInt("Column")) != n.Column {
			t.Fatalf("raw Parse audit changed at node %d", id)
		}
		children := row.GetList("content")
		if len(children) != len(n.Content) {
			t.Fatal("audit child count mismatch")
		}
		for i, child := range n.Content {
			value, _ := children[i].(jsonx.Number).Int64()
			walk(child, int(value))
		}
		walk(n.Alias, int(row.GetInt("alias")))
	}
	walk(root, 1)
	if len(seen) != len(records) {
		t.Fatal("audit includes unreachable nodes")
	}
}
