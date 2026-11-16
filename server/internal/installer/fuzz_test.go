package installer

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"configflow/internal/jsonx"
)

func readGzipJSONLines(t *testing.T, path string) []*jsonx.Object {
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<20), 64<<20)
	var out []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestFuzzCorpusMatchesPython replays testdata/fuzz.jsonl.gz (made by
// testdata/gen_fuzz.py: seeded random inputs with shell metacharacters, line
// breaks, Unicode, long values, malformed ports, addresses, URLs and paths,
// rendered by the Python generators). Every rendered script must have the
// same SHA256 and length, every refusal the same exception text.
func TestFuzzCorpusMatchesPython(t *testing.T) {
	file, err := os.Open("testdata/fuzz.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<20), 64<<20)
	total, rendered, refused, failed := 0, 0, 0, 0
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		record := value.(*jsonx.Object)
		total++
		id := record.GetInt("id")
		fn := record.GetString("fn")
		k := kwargs{record.GetObject("kwargs")}
		output, err := func() (out string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = &ValueError{Message: "panic: " + jsonx.Repr(r)}
				}
			}()
			return renderCase(fn, k)
		}()
		if failed > 40 {
			continue
		}
		if !record.GetBool("ok") {
			refused++
			if err == nil {
				failed++
				t.Errorf("case %d %s %s: python refused (%s), go rendered", id, fn, jsonx.Repr(k.Object), record.GetString("error"))
			} else if got := errorText(err); got != record.GetString("error") {
				failed++
				t.Errorf("case %d %s %s:\n  python %s\n  go     %s", id, fn, jsonx.Repr(k.Object), record.GetString("error"), got)
			}
			continue
		}
		rendered++
		if err != nil {
			failed++
			t.Errorf("case %d %s %s: python rendered, go: %v", id, fn, jsonx.Repr(k.Object), err)
			continue
		}
		digest := sha256.Sum256([]byte(output))
		if hex.EncodeToString(digest[:]) != record.GetString("sha256") || int64(len(output)) != record.GetInt("length") {
			failed++
			t.Errorf("case %d %s %s: output differs", id, fn, jsonx.Repr(k.Object))
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("fuzz corpus: %d cases, %d rendered, %d refused, %d differ", total, rendered, refused, failed)
}
