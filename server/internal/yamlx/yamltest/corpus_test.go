package yamltest

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestLegacyReaderMigrationGate(t *testing.T) {
	for _, name := range []string{"corpus.jsonl", "review.jsonl"} {
		file, err := os.Open("../testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 1<<20), 1<<27)
		var sources []string
		for scanner.Scan() {
			var row struct {
				Kind  string `json:"kind"`
				Input string `json:"input"`
				Text  string `json:"text"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
				t.Fatal(err)
			}
			if name == "corpus.jsonl" && row.Kind == "load" {
				sources = append(sources, row.Input)
			}
			if name == "review.jsonl" {
				sources = append(sources, row.Text)
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatal(err)
		}
		_ = file.Close()
		Check(t, "yamlx/"+name, sources)
	}
}
