package yamlxtest

import (
	"bufio"
	"compress/gzip"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Rows reads a synthetic JSON-lines fixture without interpreting YAML values.
func Rows(t testing.TB, path string) []*jsonx.Object {
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
	var rows []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		row, ok := value.(*jsonx.Object)
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
