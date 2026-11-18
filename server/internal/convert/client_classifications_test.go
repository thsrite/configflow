package convert_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

type clientClassification struct {
	Row      int    `json:"row"`
	Fn       string `json:"fn"`
	InputSHA string `json:"input_sha256"`
	Field    string `json:"field"`
	Family   string `json:"family"`
}
type clientClassificationIndex struct {
	SourceSHA string                 `json:"source_sha256"`
	Counts    map[string]int         `json:"counts"`
	Rows      []clientClassification `json:"rows"`
}

const clientIndexPath = "testdata/n88c_helper_classifications.json.gz"

func clientIssueFamily(issue string) string {
	switch {
	case strings.Contains(issue, "pending shared"):
		return "pending shared integer-string"
	case strings.Contains(issue, "TUIC ALPN"):
		return "Surge TUIC ALPN list bug"
	case strings.Contains(issue, "WireGuard numeric reserved"):
		return "Surge WireGuard numeric reserved bug"
	case strings.Contains(issue, "non-ASCII down"):
		return "native non-ASCII down"
	case strings.Contains(issue, "numeric zero optional text"):
		return "numeric zero optional text"
	case strings.Contains(issue, "null default") || strings.Contains(issue, "follow health"):
		return "Surge null/follow health defaults"
	case strings.Contains(issue, "null absent"):
		return "null absent"
	default:
		return "R5 or validation"
	}
}
func readClientClassifications(t *testing.T, sourceName, indexPath string) map[int]clientClassification {
	t.Helper()
	if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") == "1" {
		return nil
	}
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err = io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var index clientClassificationIndex
	if err = json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/" + sourceName)
	if err != nil || generationDigest(source) != index.SourceSHA {
		t.Fatal("helper classification source changed", err)
	}
	rows := map[int]clientClassification{}
	counts := map[string]int{}
	for _, entry := range index.Rows {
		if _, ok := rows[entry.Row]; ok {
			t.Fatal("duplicate helper classification")
		}
		if entry.InputSHA == "" || entry.Field == "" || entry.Family != clientIssueFamily(entry.Field) {
			t.Fatal("invalid helper classification")
		}
		rows[entry.Row] = entry
		counts[entry.Family]++
	}
	left, _ := json.Marshal(counts)
	right, _ := json.Marshal(index.Counts)
	if !bytes.Equal(left, right) {
		t.Fatal("helper classification counts changed")
	}
	t.Logf("explicit helper classification families: %v", counts)
	return rows
}
func writeClientClassifications(t *testing.T, sourceName, indexPath string, entries []clientClassification) {
	t.Helper()
	if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") != "1" {
		return
	}
	source, err := os.ReadFile("testdata/" + sourceName)
	if err != nil {
		t.Fatal(err)
	}
	index := clientClassificationIndex{SourceSHA: generationDigest(source), Counts: map[string]int{}, Rows: entries}
	for _, entry := range entries {
		index.Counts[entry.Family]++
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	writer := gzip.NewWriter(&encoded)
	if _, err = writer.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(indexPath, encoded.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("recorded helper input classifications only (no native outputs): %d %v", len(entries), index.Counts)
}
