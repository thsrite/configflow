package installer

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"testing"

	"configflow/internal/installer/installertest"
)

func readGzipJSONLines(t *testing.T, path string) []map[string]any {
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
	var out []map[string]any
	for scanner.Scan() {
		value, err := parseJSONMap(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, value)
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
	t.Parallel()
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
	total, rendered, refused, failed, deliberate := 0, 0, 0, 0, 0
	for scanner.Scan() {
		record, err := parseJSONMap(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		total++
		id := mapInt(record, "id")
		fn := mapStr(record, "fn")
		k := kwargs{mapObj(record, "kwargs")}
		output, err := func() (out string, err error) {
			defer func() {
				if r := recover(); r != nil {
					err = &ValidationError{Message: fmt.Sprintf("panic: %v", r)}
				}
			}()
			return renderCase(fn, k)
		}()
		if o17RareLineBreak(k.m) {
			deliberate++ // O17: scripts break lines at \n, \r\n and \r only
			continue
		}
		if handled, problem := n810ScalarMatch(record, output, err); handled {
			if problem != "" {
				failed++
				t.Errorf("case %d approved integer policy: %s", id, problem)
			}
			continue
		}
		if failed > 40 {
			continue
		}
		if err != nil && deliberateAgentNameFix(k.m, err) {
			deliberate++ // decision 6: one trailing "\n" in agent_name is refused in Go
			continue
		}
		if !mapBool(record, "ok") {
			refused++
			if err == nil && urllibOnlyRefusal(mapStr(record, "error")) {
				// accepted N2 class: urllib-only host refusal
			} else if err == nil {
				failed++
				t.Errorf("case %d %s %v: python refused (%s), go rendered", id, fn, k.m, mapStr(record, "error"))
			} else if got := errorText(err); !sameRefusal(mapStr(record, "error"), got, k.m) {
				failed++
				t.Errorf("case %d %s %v:\n  python %s\n  go     %s", id, fn, k.m, mapStr(record, "error"), got)
			}
			continue
		}
		rendered++
		if err != nil && refusedByNetURL(k.m) {
			continue // N2: a URL net/url refuses
		}
		if err != nil {
			failed++
			t.Errorf("case %d %s %v: python rendered, go: %v", id, fn, k.m, err)
			continue
		}
		output, err = installertest.PythonBaseline(output)
		if err != nil {
			t.Fatal(err)
		}
		matches, oracleErr := writerHashCaseMatches(fn, k.m, output, mapStr(record, "sha256"), mapInt(record, "length"))
		if oracleErr != nil {
			t.Fatal(oracleErr)
		}
		if !matches {
			failed++
			t.Errorf("case %d %s %v: output differs", id, fn, k.m)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	t.Logf("fuzz corpus: %d cases, %d rendered, %d refused, %d differ, %d deliberate (decision 6)", total, rendered, refused, failed, deliberate)
}
