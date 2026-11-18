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
	"configflow/internal/weburl"
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
			if err == nil {
				result = l1GuideBaseline(t, result)
			}
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
		if request := input.GetObject("request"); request != nil && err != nil {
			if l1CompactColonRefusal(request, err) {
				counts["deliberate (L1 D1 compact colon refusal)"]++
				continue
			}
			prior := record.GetObject("error")
			if prior != nil && l1CompactPathWording(request, []any{prior.GetString("class"), prior.GetString("message")}, err) {
				counts["deliberate (L1 D1 compact path wording)"]++
				continue
			}
		}
		if request, _ := input.Value("request").(*jsonx.Object); err != nil && request != nil && deliberateAgentNameFix(request, err, "agent_name", "existing_agent_name") {
			// Decision 6: one trailing "\n" in an Agent name is refused in Go.
			if !(record.GetBool("ok") == false && record.GetObject("error").GetString("message") == invalidAgentNameMessage) {
				counts["deliberate (agent name trailing newline)"]++
				continue
			}
		}
		if err != nil && deliberateServerURLFix(input, err) {
			// Deliberate fix of a Python validation bug (A09 decision): one
			// trailing "\n" in server_url is refused in Go.
			if !(record.GetBool("ok") == false && record.GetObject("error").GetString("message") == invalidServerMessage) {
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
		var writerReference any
		var writerReferencePresent bool
		if record.GetString("kind") == "guide" {
			var oracleErr error
			writerReference, writerReferencePresent, oracleErr = writerExpected("generate_guide", []any{input.Value("request"), input.Value("enrollment")})
			if oracleErr != nil {
				t.Fatal(oracleErr)
			}
		}
		if writerReferencePresent {
			if !nativeOutcomeEqual(writerReference, result, "") {
				fail("native writer reference differs")
				continue
			}
		} else if digests := record.GetObject("digests"); digests != nil {
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
				if digestOf(t, result.Value(key)) != digests.GetString(key) && !nativeFieldMatches(key, result.Value(key), digests.GetString(key)) {
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

const invalidServerMessage = "请填写完整的 ConfigFlow HTTP(S) 地址"

// deliberateServerURLFix reports whether a guide request's server_url ends
// with exactly one "\n" and Go refused it as an invalid server address.
func deliberateServerURLFix(input *jsonx.Object, err error) bool {
	request, _ := input.Value("request").(*jsonx.Object)
	server, _ := request.GetOr("server_url", nil).(string)
	ve, isValue := err.(*ValueError)
	return strings.HasSuffix(server, "\n") && !strings.HasSuffix(server, "\n\n") && isValue && ve.Message == invalidServerMessage
}

// TestServerURLTrailingNewlineIsRefused: deliberate fix of a Python
// validation bug (maintainer decision 2026-11-16). Python's
// re.match(r'^https?://[^\s]+$') accepts "http://x.test\n" and writes the
// newline into the guide; Go refuses it like any other invalid address.
func TestServerURLTrailingNewlineIsRefused(t *testing.T) {
	base := jsonx.Obj("server_url", "http://config.test:8000", "agent_name", "dns-one", "agent_ip", "192.168.10.20",
		"data_dir", "/srv/cf/new-data", "ha_host_id_path", "/srv/cf/host-id")
	if _, err := GenerateGuide(base, nil); err != nil {
		t.Fatal(err)
	}
	for _, server := range []string{"http://config.test:8000\n", "https://config.test/\n", "http://config.test\n\n", "http://x\r\n"} {
		request := base.DeepCopy()
		request.Set("server_url", server)
		for _, mode := range []string{"new", "migrate"} {
			request.Set("mode", mode)
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
	case *jsonx.Object:
		for _, key := range v.Keys() {
			if refusedByNetURL(v.Value(key)) {
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
