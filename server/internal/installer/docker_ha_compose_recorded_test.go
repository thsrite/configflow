package installer

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

func readComposeLines(t *testing.T, path string) []*jsonx.Object {
	t.Helper()
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
	scanner.Buffer(make([]byte, 1<<20), 1<<28)
	var rows []*jsonx.Object
	for scanner.Scan() {
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, value.(*jsonx.Object))
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// replayGenerator runs one recorded generate_compact / generate_direct /
// generate_bundle call in Go and describes any difference ("" = equal).
// With digestBundle the Python bundle carries content_sha256 instead of
// content_base64.
func replayGenerator(row *jsonx.Object, digestBundle bool) (string, string) {
	args := row.GetList("args")
	params, _ := jsonx.DeepCopy(args[0]).(*jsonx.Object)
	request, _ := jsonx.DeepCopy(args[1]).(*jsonx.Object)
	var enrollment *Enrollment
	if e, _ := args[2].(*jsonx.Object); e != nil && e.Len() > 0 {
		enrollment = &Enrollment{URL: e.GetString("url"), Token: e.GetString("token"), ExpiresAt: e.GetOr("expires_at", nil)}
	}
	var text string
	var info *jsonx.Object
	var err error
	fn := row.GetString("fn")
	switch fn {
	case "generate_compact":
		text, info, err = GenerateCompact(params, request, enrollment)
		if strings.Contains(params.GetString("data_dir"), ":") && err != nil && text == "" && info == nil {
			var ve *ValueError
			if errors.As(err, &ve) && ve.Message == l1CompactPathMessage {
				return "deliberate (L1 D1 compact colon)", ""
			}
		}
	case "generate_direct":
		text, info, err = GenerateDirect(params, request, enrollment)
	case "generate_bundle":
		text, info, err = GenerateBundle(params, request, enrollment)
	default:
		return "unknown", "unknown function " + fn
	}
	if err != nil && deliberateAgentNameFix(params, err) {
		if pyErr := row.GetList("err"); pyErr == nil || pyErr[1] != invalidAgentNameMessage {
			return "deliberate (decision 6)", ""
		}
	}
	if pyErr := row.GetList("err"); pyErr != nil {
		if err == nil {
			return "differ", fmt.Sprintf("python %v %q, go answered", pyErr[0], pyErr[1])
		}
		class, message := pythonClassOf(err)
		pyClass := "other"
		if pyErr[0] == "ValueError" {
			pyClass = "value"
		}
		if class != pyClass || (class == "value" && message != pyErr[1]) {
			return "differ", fmt.Sprintf("python %v %q, go %s %q", pyErr[0], pyErr[1], class, message)
		}
		return "refused", ""
	}
	if err != nil {
		return "differ", fmt.Sprintf("python answered, go error %v", err)
	}
	out := row.GetList("out")
	if value, present, oracleErr := writerExpected(fn, row.Value("args")); oracleErr != nil {
		return "oracle", oracleErr.Error()
	} else if present {
		var ok bool
		out, ok = value.([]any)
		if !ok || len(out) != 2 {
			return "oracle", "invalid generator reference shape"
		}
	}
	if text != out[0] && !writerYAMLEqual(out[0].(string), text) {
		return "differ", "text differs: " + firstDifference(out[0].(string), text)
	}

	if !nativeOutcomeEqual(out[1], info, "") {
		if a, b := info.GetString("content_base64"), out[1].(*jsonx.Object).GetString("content_base64"); a != b {
			return "differ", "bundle zip differs: " + zipDifference(b, a)
		}
		return "differ", fmt.Sprintf("info python %s go %s", jsonx.Repr(out[1]), jsonx.Repr(info))
	}
	return "answered", ""
}

func zipDifference(pyB64, goB64 string) string {
	a, _ := base64.StdEncoding.DecodeString(pyB64)
	b, _ := base64.StdEncoding.DecodeString(goB64)
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("first byte %d of %d/%d", i, len(a), len(b))
		}
	}
	return fmt.Sprintf("lengths %d/%d", len(a), len(b))
}

// TestComposeRecordedCallsMatchPython replays testdata/docker_ha_compose_recorded.jsonl.gz:
// every generate_compact / generate_direct / generate_bundle call made while
// the whole Python backend suite ran under testdata/a10_record.py.
func TestComposeRecordedCallsMatchPython(t *testing.T) {
	t.Parallel()
	rows := readComposeLines(t, "testdata/docker_ha_compose_recorded.jsonl.gz")
	counts := map[string]int{}
	tests := map[string]bool{}
	failed := 0
	for _, row := range rows {
		tests[row.GetString("test")] = true
		outcome, detail := replayGenerator(row, false)
		if outcome == "differ" {
			failed++
			if failed <= 20 {
				t.Errorf("%s %s: %s", row.GetString("test"), row.GetString("fn"), detail)
			}
		}
		counts[row.GetString("fn")+" "+outcome]++
	}
	t.Logf("recorded A10 calls: %d from %d tests, %v, %d differ", len(rows), len(tests), counts, failed)
	if len(rows) == 0 {
		t.Fatal("no recorded calls")
	}
}

// TestComposeCorpusMatchesPython replays testdata/docker_ha_compose.jsonl.gz
// (testdata/gen_docker_ha_compose.py on CPython 3.11 / zlib 1.3): the three
// generators over fixed and seeded random params, requests and enrollments,
// and zipfile's raw deflate over many inputs.
func TestComposeCorpusMatchesPython(t *testing.T) {
	t.Parallel()
	rows := readComposeLines(t, "testdata/docker_ha_compose.jsonl.gz")
	counts := map[string]int{}
	failed := 0
	for _, row := range rows {
		if row.GetString("kind") == "deflate" {
			input, _ := base64.StdEncoding.DecodeString(row.GetString("input"))
			out := zlibDeflate(input)
			sum := sha256.Sum256(out)
			if hex.EncodeToString(sum[:]) != row.GetString("sha256") {
				failed++
				t.Errorf("deflate case %v (%d bytes): python %d bytes, go %d", row.Value("id"), len(input), row.GetInt("size"), len(out))
			}
			counts["deflate"]++
			continue
		}
		outcome, detail := replayGenerator(row, true)
		if outcome == "differ" {
			failed++
			if failed <= 20 {
				t.Errorf("case %v %s %.400s: %s", row.Value("id"), row.GetString("fn"), jsonx.Repr(row.Value("args")), detail)
			}
		}
		counts[row.GetString("fn")+" "+outcome]++
	}
	t.Logf("A10 corpus: %d cases, %v, %d differ", len(rows), counts, failed)
}

// TestComposeBootstrapMatchesPython: the generated text constants equal the
// raw BOOTSTRAP literal of backend/agents/docker_ha_compose.py.
func TestComposeBootstrapMatchesPython(t *testing.T) {
	source, err := os.ReadFile("../../../backend/agents/docker_ha_compose.py")
	if err != nil {
		t.Skip("backend source not available:", err)
	}
	_, rest, found := strings.Cut(string(source), "BOOTSTRAP = r'''")
	literal, _, closed := strings.Cut(rest, "'''")
	if !found || !closed || literal != composeBootstrap {
		t.Fatal("composeBootstrap differs from BOOTSTRAP; rerun testdata/gen_compose_text.py")
	}
	for i, block := range composePrepareBlocks {
		if !strings.Contains(string(source), "r'''"+block+"'''") {
			t.Errorf("prepare block %d differs from docker_ha_compose.py; rerun testdata/gen_compose_text.py", i)
		}
	}
}
