package yamlxtest

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Rows reads a synthetic JSON-lines fixture without interpreting YAML values.
func Rows(t testing.TB, path string) []map[string]any {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var scanner *bufio.Scanner
	if strings.HasSuffix(path, ".gz") {
		reader, err := gzip.NewReader(file)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		scanner = bufio.NewScanner(reader)
	} else {
		scanner = bufio.NewScanner(file)
	}
	scanner.Buffer(make([]byte, 65536), 1<<27)
	var rows []map[string]any
	for scanner.Scan() {
		dec := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			t.Fatal(err)
		}
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatal("fixture row is not an object")
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}
