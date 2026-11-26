package convert_test

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"configflow/internal/yamlx"
)

type numericSpelling struct {
	RowSHA256          string `json:"row_sha256"`
	OldByteSHA256      string `json:"old_byte_sha256"`
	Field              string `json:"field"`
	Old                string `json:"old"`
	Native             string `json:"native"`
	PythonOutputSHA256 string `json:"python_output_sha256"`
}

func numericSpellingReferences(t *testing.T) map[string]numericSpelling {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_numeric_spellings.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		ArchiveSHA256  string                     `json:"archive_sha256"`
		ManifestSHA256 string                     `json:"manifest_sha256"`
		Rows           map[string]numericSpelling `json:"rows"`
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile("testdata/mihomo_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile("testdata/n88_generation_bytes.json")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(archive)) != ref.ArchiveSHA256 || fmt.Sprintf("%x", sha256.Sum256(manifest)) != ref.ManifestSHA256 || len(ref.Rows) != 20 {
		t.Fatal("numeric spelling baseline changed")
	}
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 65536), 1<<26)
	used := 0
	for i := 0; scanner.Scan(); i++ {
		if row, ok := ref.Rows[strconv.Itoa(i)]; ok {
			if fmt.Sprintf("%x", sha256.Sum256(scanner.Bytes())) != row.RowSHA256 || row.PythonOutputSHA256 == "" {
				t.Fatal("numeric spelling lost original row binding", i)
			}
			used++
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if used != 20 {
		t.Fatal("numeric spelling reference not fully bound")
	}
	return ref.Rows
}

// numericByteReference changes only an exact numeric scalar's presentation.
// Complete generated semantics, typed/legacy agreement, and every other output
// byte continue to be checked by the existing corpus and immutable manifest.
func numericByteReference(t *testing.T, output string, ref numericSpelling, oldSHA string) string {
	t.Helper()
	if ref.OldByteSHA256 != oldSHA {
		t.Fatal("numeric original byte hash moved")
	}
	if !(ref.Field == "number" && ref.Old == "1e+20" && ref.Native == "1.0e+20") && !(ref.Field == "text" && ref.Old == "1000.0" && ref.Native == "1e3") {
		t.Fatal("unexpected numeric spelling pair")
	}
	parsed, err := yamlx.Parse([]byte(output))
	if err != nil {
		t.Fatal(err)
	}
	edited, err := yamlx.Edit([]byte(output), func(node *yamlx.Node) error {
		value := yamlx.Get(node, ref.Field)
		if value == nil || value.ShortTag() != "!!float" || value.Value != ref.Native {
			t.Fatal("numeric reference no longer binds float scalar")
		}
		old, err := strconv.ParseFloat(ref.Old, 64)
		if err != nil {
			return err
		}
		current, err := strconv.ParseFloat(value.Value, 64)
		if err != nil {
			return err
		}
		if old != current {
			t.Fatal("numeric spelling changed value")
		}
		value.Value = ref.Old
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	compared, err := yamlx.Parse(edited)
	if err != nil || !yamlx.Equal(parsed, compared) {
		t.Fatal("numeric byte projection changed consumer semantics", err)
	}
	return string(edited)
}
