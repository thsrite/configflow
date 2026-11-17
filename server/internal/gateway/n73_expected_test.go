package gateway_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func nativeGatewayReferences(t *testing.T) *jsonx.Object {
	t.Helper()
	fixture := yamlxtest.Fixture(t, "testdata/n73_payload_expected.json.gz")
	data, err := os.ReadFile("testdata/a19_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	stats := fixture.GetObject("stats")
	if hex.EncodeToString(sum[:]) != fixture.GetString("corpus_sha256") || stats.GetInt("changed") != 4 || stats.GetInt("classified") != 4 || stats.GetInt("unknown") != 0 || fixture.GetObject("cases").Len() != 4 {
		t.Fatal("invalid or changed native gateway reference")
	}
	return fixture.GetObject("cases")
}

func nativeGatewayRow(t *testing.T, index int, row corpusRow, references *jsonx.Object) corpusRow {
	t.Helper()
	ref := references.GetObject(fmt.Sprintf("gateway/a19/%d", index))
	if ref == nil {
		return row
	}
	args, err := jsonx.Parse(row.Args)
	if err != nil || row.FN != "extract" || !yamlxtest.EqualJSON(args, ref.Value("args")) {
		t.Fatal("native gateway reference no longer matches its exact caller")
	}
	old := jsonx.Obj()
	if row.Err != nil {
		old.Set("err", []any{row.Err[0], row.Err[1]})
	} else {
		out, err := jsonx.Parse(row.Out)
		if err != nil {
			t.Fatal(err)
		}
		old.Set("out", out)
	}
	if !yamlxtest.EqualJSON(old, ref.Value("old")) {
		t.Fatal("original Python gateway golden changed")
	}
	expected := ref.GetObject("expected")
	row.Err, row.Out = nil, nil
	if errors := expected.GetList("err"); errors != nil {
		row.Err = []string{errors[0].(string), errors[1].(string)}
	} else {
		data, err := jsonx.Compact(expected.Value("out"))
		if err != nil {
			t.Fatal(err)
		}
		row.Out = json.RawMessage(data)
	}
	return row
}

func TestN73GatewayLoaderOracleProvenance(t *testing.T) {
	references := nativeGatewayReferences(t)
	if references.Len() != 4 {
		t.Fatal("missing explicit gateway changes")
	}
	yamlxtest.CheckReaderFixtures(t, "testdata/n73_payload_native.json.gz", "testdata/n73_payload_expected.json.gz", map[string]func(*yamlx.Node) error{
		"gateway-payload": func(*yamlx.Node) error { return nil },
	})
}
