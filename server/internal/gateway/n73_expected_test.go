package gateway_test

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

type nativeGatewayCase struct {
	Args     json.RawMessage `json:"args"`
	Old      json.RawMessage `json:"old"`
	Expected struct {
		Err []string        `json:"err"`
		Out json.RawMessage `json:"out"`
	} `json:"expected"`
}

func nativeGatewayReferences(t *testing.T) map[string]nativeGatewayCase {
	t.Helper()
	file, err := os.Open("testdata/n73_payload_expected.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	var fixture struct {
		CorpusSHA256 string `json:"corpus_sha256"`
		Stats        struct {
			Changed    int `json:"changed"`
			Classified int `json:"classified"`
			Unknown    int `json:"unknown"`
		} `json:"stats"`
		Cases map[string]nativeGatewayCase `json:"cases"`
	}
	if err := json.NewDecoder(reader).Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/a19_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != fixture.CorpusSHA256 || fixture.Stats.Changed != 4 || fixture.Stats.Classified != 4 || fixture.Stats.Unknown != 0 || len(fixture.Cases) != 4 {
		t.Fatal("invalid or changed native gateway reference")
	}
	return fixture.Cases
}

func nativeGatewayRow(t *testing.T, index int, row corpusRow, references map[string]nativeGatewayCase) corpusRow {
	t.Helper()
	ref, ok := references[fmt.Sprintf("gateway/a19/%d", index)]
	if !ok {
		return row
	}
	rowArgs, err := testjson.Parse(row.Args)
	if err != nil {
		t.Fatal(err)
	}
	refArgs, err := testjson.Parse(ref.Args)
	if err != nil {
		t.Fatal(err)
	}
	if row.FN != "extract" || !testjson.Equal(rowArgs, refArgs) {
		t.Fatal("native gateway reference no longer matches its exact caller")
	}
	old := map[string]any{}
	if row.Err != nil {
		old["err"] = []any{row.Err[0], row.Err[1]}
	} else {
		out, err := testjson.Parse(row.Out)
		if err != nil {
			t.Fatal(err)
		}
		old["out"] = out
	}
	refOld, err := testjson.Parse(ref.Old)
	if err != nil {
		t.Fatal(err)
	}
	if !testjson.Equal(old, refOld) {
		t.Fatal("original Python gateway golden changed")
	}
	row.Err, row.Out = nil, nil
	if ref.Expected.Err != nil {
		row.Err = append([]string(nil), ref.Expected.Err...)
	} else {
		row.Out = ref.Expected.Out
	}
	return row
}

func TestN73GatewayLoaderOracleProvenance(t *testing.T) {
	references := nativeGatewayReferences(t)
	if len(references) != 4 {
		t.Fatal("missing explicit gateway changes")
	}
	yamlxtest.CheckReaderFixtures(t, "testdata/n73_payload_native.json.gz", "testdata/n73_payload_expected.json.gz", map[string]func(*yamlx.Node) error{
		"gateway-payload": func(*yamlx.Node) error { return nil },
	})
}
