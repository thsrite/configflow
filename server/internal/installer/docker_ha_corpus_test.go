package installer

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sort"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

func readDockerHACorpus(t *testing.T) []*jsonx.Object {
	t.Helper()
	file, err := os.Open("testdata/docker_ha.jsonl.gz")
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

func digestOf(t *testing.T, v any) string {
	data, err := jsonx.Encode(v, jsonx.Options{SortKeys: true, Compact: true})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:24]
}

// pythonClassOf tells how the routes treat err: "value" (ValueError, 400),
// "other" (500), "a10" (Compose variant not ported yet).
func pythonClassOf(err error) (string, string) {
	if errors.Is(err, ErrComposeVariantNotPorted) {
		return "a10", ""
	}
	var ve *ValueError
	var ee *EnrollmentError
	switch {
	case errors.As(err, &ve):
		return "value", ve.Message
	case errors.As(err, &ee):
		return "value", ee.Message
	}
	return "other", err.Error()
}

// TestDockerHACorpusMatchesPython replays testdata/docker_ha.jsonl.gz (made
// by testdata/gen_docker_ha.py on CPython 3.11): generate_guide over a broad
// request matrix (read-only steps, new installations host/macvlan with and
// without enrollment, migrations, malformed and non-string values, seeded
// random combinations) and parse_report over the test file's outputs plus
// many realistic and damaged ones. Answers and ValueError texts must match;
// a Python exception of another class must be a non-ValueError in Go.
func TestDockerHACorpusMatchesPython(t *testing.T) {
	records := readDockerHACorpus(t)
	counts := map[string]int{}
	failed := 0
	for _, record := range records {
		id := jsonx.Str(record.Value("id"))
		input, _ := record.Value("input").(*jsonx.Object)
		var result *jsonx.Object
		var err error
		switch record.GetString("kind") {
		case "b64":
			decoded, ok := b64decodeValidate(record.GetString("input"))
			if ok != record.GetBool("ok") || (ok && hex.EncodeToString(decoded) != record.GetString("result")) {
				failed++
				t.Errorf("b64 %q: python %v %v, go %v %x", record.GetString("input"), record.GetBool("ok"), record.Value("result"), ok, decoded)
			}
			counts["base64"]++
			continue
		case "guide":
			var enrollment *Enrollment
			if e := input.GetObject("enrollment"); e != nil {
				enrollment = &Enrollment{URL: e.GetString("url"), Token: e.GetString("token")}
			}
			result, err = GenerateGuide(jsonx.DeepCopy(input.Value("request")), enrollment)
		default:
			mode := input.GetOr("mode", "host")
			result, err = ParsePreflightReport(input.Value("output"), mode)
		}
		fail := func(format string, args ...any) {
			failed++
			if failed <= 25 {
				t.Errorf("case %s (%s %.300s): "+format, append([]any{id, record.GetString("kind"), jsonx.Repr(input)}, args...)...)
			}
		}
		if err != nil {
			class, message := pythonClassOf(err)
			if class == "a10" {
				counts["skipped (A10)"]++
				continue
			}
			if record.GetBool("ok") {
				fail("python answered, go error %v", err)
				continue
			}
			pyError := record.GetObject("error")
			pyClass := "other"
			if pyError.GetBool("value_error") {
				pyClass = "value"
			}
			if class != pyClass || (class == "value" && message != pyError.GetString("message")) {
				fail("python %s %q, go %s %q", pyError.GetString("class"), pyError.GetString("message"), class, message)
				continue
			}
			counts["refused"]++
			continue
		}
		if !record.GetBool("ok") {
			fail("python %v, go answered", record.Value("error"))
			continue
		}
		if digests := record.GetObject("digests"); digests != nil {
			var keys []string
			for key := range result.All() {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			pyKeys := digests.Keys()
			sort.Strings(pyKeys)
			if strings.Join(keys, ",") != strings.Join(pyKeys, ",") {
				fail("keys python %v go %v", pyKeys, keys)
				continue
			}
			var differ []string
			for _, key := range keys {
				if digestOf(t, result.Value(key)) != digests.GetString(key) {
					differ = append(differ, key)
				}
			}
			if len(differ) > 0 {
				detail := ""
				for _, key := range differ {
					if text, ok := result.Value(key).(string); ok {
						detail += "\n--- go " + key + ":\n" + text
					}
				}
				fail("keys differ %v%.3000s", differ, detail)
				continue
			}
		} else if !jsonx.Equal(result, record.Value("result")) {
			fail("python %s\ngo     %s", jsonx.Repr(record.Value("result")), jsonx.Repr(result))
			continue
		}
		counts["answered"]++
	}
	t.Logf("docker HA corpus: %d cases, %v, %d differ", len(records), counts, failed)
	if failed > 0 {
		t.Fatalf("%d of %d cases differ", failed, len(records))
	}
}
