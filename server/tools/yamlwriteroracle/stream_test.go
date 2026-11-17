package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestRecordedGraphWriterStream(t *testing.T) {
	input := `{"nodes":[{"kind":4,"tag":"!!map","children":[1,2]},{"kind":8,"tag":"!!str","value":"image"},{"kind":8,"tag":"!!str","value":"0o17","anchor":"agent_image"}]}`
	var output bytes.Buffer
	if err := writerStream(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var answer struct {
		YAML  string `json:"yaml"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(output.Bytes(), &answer); err != nil || answer.Error != "" {
		t.Fatalf("stream: %v %s", err, answer.Error)
	}
	n, err := yamlx.Parse([]byte(answer.YAML))
	if err != nil || !yamlx.Equal(n, yamlx.Map(yamlx.Str("image"), yamlx.Str("0o17"))) {
		t.Fatalf("recorded string changed type: %s: %v", answer.YAML, err)
	}
}

func TestWriterChangeClassification(t *testing.T) {
	for _, cp := range []rune{'\u0085', '\u2028', '\u2029'} {
		old := "path: 'a b'\n"
		data, err := yamlx.Marshal(yamlx.Map(yamlx.Str("path"), yamlx.Str("a"+string(cp)+"b")))
		if err != nil {
			t.Fatal(err)
		}
		request, _ := json.Marshal(map[string]string{"op": "compare", "old": old, "new": string(data)})
		var output bytes.Buffer
		if err := writerStream(bytes.NewReader(request), &output); err != nil {
			t.Fatal(err)
		}
		var answer struct {
			Changes []yamlxtest.WriterChange `json:"changes"`
			Error   string                   `json:"error"`
		}
		if err := json.Unmarshal(output.Bytes(), &answer); err != nil || answer.Error != "" || len(answer.Changes) != 1 {
			t.Fatalf("classification: %v %s", err, answer.Error)
		}
		change := answer.Changes[0]
		if change.Category != "unicode-line-separator-preservation" || len(change.Restored) != 1 || change.Restored[fmt.Sprintf("U+%04X", cp)] != 1 {
			t.Fatalf("wrong character category: %+v", change)
		}
	}
	for _, pair := range [][2]string{{"a: 1", "a: 2"}, {"a: one", "a: two"}, {"a: []", "a: [one]"}, {"a: 1", "a: 1\nextra: true"}} {
		request, _ := json.Marshal(map[string]string{"op": "compare", "old": pair[0], "new": pair[1]})
		var output bytes.Buffer
		if err := writerStream(bytes.NewReader(request), &output); err != nil {
			t.Fatal(err)
		}
		var answer map[string]any
		if err := json.Unmarshal(output.Bytes(), &answer); err != nil || answer["error"] == nil {
			t.Fatalf("unrelated change was classified: %s: %v", output.Bytes(), err)
		}
	}
}
