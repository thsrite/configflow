package installer

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/testjson"
	"configflow/internal/weburl"
)

func readDockerHACorpus(t *testing.T) []map[string]any {
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

func digestOf(t *testing.T, v any) string {
	data, err := sortedCompactJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:24]
}

// pythonClassOf tells how the routes treat err: "value" (ValueError, 400)
// or "other" (500).
func pythonClassOf(err error) (string, string) {
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
	t.Parallel()
	records := readDockerHACorpus(t)
	counts := map[string]int{}
	failed := 0
	for _, record := range records {
		id := fmt.Sprint(record["id"])
		input, _ := record["input"].(map[string]any)
		var result map[string]any
		var err error
		switch mapStr(record, "kind") {
		case "b64":
			decoded, ok := b64decodeValidate(mapStr(record, "input"))
			if expected, present, err := n810Base64Expected(record); err != nil {
				t.Fatal(err)
			} else if present {
				if ok != mapBool(expected, "ok") || (ok && hex.EncodeToString(decoded) != mapStr(expected, "result")) || (!ok && len(decoded) != 0) {
					t.Fatal("native base64 grammar differs")
				}
				counts["approved native base64"]++
				continue
			}
			if ok != mapBool(record, "ok") || (ok && hex.EncodeToString(decoded) != mapStr(record, "result")) {
				failed++
				t.Errorf("b64 %q: python %v %v, go %v %x", mapStr(record, "input"), mapBool(record, "ok"), record["result"], ok, decoded)
			}
			counts["base64"]++
			continue
		case "guide":
			var enrollment *Enrollment
			if e := mapObj(input, "enrollment"); e != nil {
				enrollment = &Enrollment{URL: mapStr(e, "url"), Token: mapStr(e, "token")}
			}
			result, err = GenerateGuide(cloneJSON(input["request"]), enrollment)
			if err == nil {
				result = l1GuideBaseline(t, result)
			}
		default:
			mode := mapGetOr(input, "mode", "host")
			result, err = ParsePreflightReport(input["output"], mode)
		}
		fail := func(format string, args ...any) {
			failed++
			if failed <= 25 {
				t.Errorf("case %s (%s %.300v): "+format, append([]any{id, mapStr(record, "kind"), input}, args...)...)
			}
		}
		if present, problem := n810PathRefusal(record, err); problem != nil {
			t.Fatal(problem)
		} else if present {
			counts["approved native path overlap"]++
			continue
		}
		if expected, present, problem := n810PreflightExpected(record); problem != nil {
			t.Fatal(problem)
		} else if present {
			if err != nil || !testjson.Equal(expected, result) {
				fail("approved native report differs: %v", err)
			}
			counts["approved native machine grammar"]++
			continue
		}
		if request := mapObj(input, "request"); request != nil && err != nil {
			if l1CompactColonRefusal(request, err) {
				counts["deliberate (L1 D1 compact colon refusal)"]++
				continue
			}
			prior := mapObj(record, "error")
			if prior != nil && l1CompactPathWording(request, []any{mapStr(prior, "class"), mapStr(prior, "message")}, err) {
				counts["deliberate (L1 D1 compact path wording)"]++
				continue
			}
		}
		if request, _ := input["request"].(map[string]any); err != nil && request != nil && deliberateAgentNameFix(request, err, "agent_name", "existing_agent_name") {
			// Decision 6: one trailing "\n" in an Agent name is refused in Go.
			if !(mapBool(record, "ok") == false && mapStr(mapObj(record, "error"), "message") == invalidAgentNameMessage) {
				counts["deliberate (agent name trailing newline)"]++
				continue
			}
		}
		if err != nil && deliberateServerURLFix(input, err) {
			// Deliberate fix of a Python validation bug (A09 decision): one
			// trailing "\n" in server_url is refused in Go.
			if !(mapBool(record, "ok") == false && mapStr(mapObj(record, "error"), "message") == invalidServerMessage) {
				counts["deliberate (server_url trailing newline)"]++
				continue
			}
		}
		if err != nil && refusedByNetURL(input) {
			counts["accepted (N2: URL net/url refuses)"]++
			continue
		}
		if err != nil {
			class, message := pythonClassOf(err)
			if mapBool(record, "ok") {
				fail("python answered, go error %v", err)
				continue
			}
			pyError := mapObj(record, "error")
			pyClass := "other"
			if mapBool(pyError, "value_error") {
				pyClass = "value"
			}
			if class != pyClass || (class == "value" && message != mapStr(pyError, "message")) {
				fail("python %s %q, go %s %q", mapStr(pyError, "class"), mapStr(pyError, "message"), class, message)
				continue
			}
			counts["refused"]++
			continue
		}
		if !mapBool(record, "ok") {
			fail("python %v, go answered", record["error"])
			continue
		}
		var writerReference any
		var writerReferencePresent bool
		if mapStr(record, "kind") == "guide" {
			var oracleErr error
			writerReference, writerReferencePresent, oracleErr = writerExpected("generate_guide", []any{input["request"], input["enrollment"]})
			if oracleErr != nil {
				t.Fatal(oracleErr)
			}
		}
		if writerReferencePresent {
			if !nativeOutcomeEqual(writerReference, result, "") {
				fail("native writer reference differs")
				continue
			}
		} else if digests := mapObj(record, "digests"); digests != nil {
			keys := mapKeys(result)
			pyKeys := mapKeys(digests)
			if strings.Join(keys, ",") != strings.Join(pyKeys, ",") {
				fail("keys python %v go %v", pyKeys, keys)
				continue
			}
			var differ []string
			for _, key := range keys {
				if digestOf(t, result[key]) != mapStr(digests, key) && !nativeFieldMatches(key, result[key], mapStr(digests, key)) {
					differ = append(differ, key)
				}
			}
			if len(differ) > 0 {
				detail := ""
				for _, key := range differ {
					if text, ok := result[key].(string); ok {
						detail += "\n--- go " + key + ":\n" + text
					}
				}
				fail("keys differ %v%.3000s", differ, detail)
				continue
			}
		} else if !testjson.Equal(result, record["result"]) {
			fail("python %v\ngo     %v", record["result"], result)
			continue
		}
		counts["answered"]++
	}
	t.Logf("docker HA corpus: %d cases, %v, %d differ", len(records), counts, failed)
	if failed > 0 {
		t.Fatalf("%d of %d cases differ", failed, len(records))
	}
}

const invalidServerMessage = "请填写完整的 ConfigFlow HTTP(S) 地址"

// deliberateServerURLFix reports whether a guide request's server_url ends
// with exactly one "\n" and Go refused it as an invalid server address.
func deliberateServerURLFix(input map[string]any, err error) bool {
	request, _ := input["request"].(map[string]any)
	server, _ := mapGetOr(request, "server_url", nil).(string)
	ve, isValue := err.(*ValueError)
	return strings.HasSuffix(server, "\n") && !strings.HasSuffix(server, "\n\n") && isValue && ve.Message == invalidServerMessage
}

// TestServerURLTrailingNewlineIsRefused: deliberate fix of a Python
// validation bug (maintainer decision 2026-11-16). Python's
// re.match(r'^https?://[^\s]+$') accepts "http://x.test\n" and writes the
// newline into the guide; Go refuses it like any other invalid address.
func TestServerURLTrailingNewlineIsRefused(t *testing.T) {
	base := map[string]any{
		"server_url":      "http://config.test:8000",
		"agent_name":      "dns-one",
		"agent_ip":        "192.168.10.20",
		"data_dir":        "/srv/cf/new-data",
		"ha_host_id_path": "/srv/cf/host-id",
	}
	if _, err := GenerateGuide(base, nil); err != nil {
		t.Fatal(err)
	}
	for _, server := range []string{"http://config.test:8000\n", "https://config.test/\n", "http://config.test\n\n", "http://x\r\n"} {
		request := cloneMap(base)
		request["server_url"] = server
		for _, mode := range []string{"new", "migrate"} {
			request["mode"] = mode
			_, err := GenerateGuide(request, nil)
			ve, ok := err.(*ValueError)
			if !ok || ve.Message != invalidServerMessage {
				t.Errorf("%q %s: %v", server, mode, err)
			}
		}
	}
}

// refusedByNetURL reports whether v holds a URL string ("://") that net/url
// refuses although urllib.parse accepted it (control characters, a
// malformed percent escape or port, unbalanced brackets). Since N2 such
// server/enrollment URLs are rejected; accepted difference class.
func refusedByNetURL(v any) bool {
	switch v := v.(type) {
	case string:
		if !strings.Contains(v, "://") {
			return false
		}
		_, err := weburl.Parse(v)
		return err != nil
	case []any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range v {
			if refusedByNetURL(item) {
				return true
			}
		}
	}
	return false
}

// sameRefusal compares a Python refusal ("Class: message") with Go's: equal,
// or the input holds a URL net/url refuses (N2), or the same class with a
// non-Chinese Python text (exception texts are not compared).
func sameRefusal(python, got string, input any) bool {
	if python == got || refusedByNetURL(input) || urllibOnlyRefusal(python) {
		return true
	}
	pyClass, pyMessage, _ := strings.Cut(python, ": ")
	goClass, _, _ := strings.Cut(got, ": ")
	return pyClass == goClass && (strings.HasSuffix(pyMessage, "does not appear to be an IPv4 or IPv6 address") || !strings.ContainsFunc(pyMessage, func(r rune) bool { return r >= 0x4e00 && r <= 0x9fff }))
}

// urllibOnlyRefusal reports a Python refusal that came from urllib's own
// host checks (accepted N2 class, review N2): hosts whose NFKC form holds a
// URL delimiter and malformed bracketed hosts (zones with "%", IPvFuture).
// net/url accepts or refuses these on its own terms; no realistic server URL
// is affected.
func urllibOnlyRefusal(python string) bool {
	for _, text := range []string{"contains invalid characters under NFKC normalization",
		"does not appear to be an IPv4 or IPv6 address", "IPvFuture address is invalid"} {
		if strings.Contains(python, text) {
			return true
		}
	}
	return false
}
