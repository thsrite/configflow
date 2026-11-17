package importer_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"configflow/internal/importer"
)

func FuzzClientImport(f *testing.F) {
	clients := []string{"mihomo", "surge", "loon", "shadowrocket"}
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		f.Fatal(err)
	}
	defer file.Close()
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 65536), 4<<20)
	for scan.Scan() {
		var row corpusRow
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			f.Fatal(err)
		}
		for i, client := range clients {
			if row.Client == client {
				f.Add(uint8(i), []byte(row.Content))
			}
		}
	}
	if err := scan.Err(); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, kind uint8, content []byte) {
		// The public HTTP edge caps API bodies at 1 MiB. Larger direct-library
		// inputs are measured separately by the isolated resource probes.
		if len(content) > 1<<20 {
			t.Skip()
		}
		plan, err := importer.Parse(clients[int(kind)%len(clients)], string(content))
		if err != nil {
			return
		}
		counter := 0
		result, err := importer.Build(plan, importer.Shared{}, importer.BuildOptions{NewID: func(prefix string) (string, error) { counter++; return fmt.Sprintf("%s_%d", prefix, counter), nil }})
		if err != nil {
			return
		}
		if _, err := json.Marshal(result); err != nil {
			t.Fatalf("successful import produced invalid JSON: %v", err)
		}
	})
}
