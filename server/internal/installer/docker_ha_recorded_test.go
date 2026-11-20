package installer

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// TestDockerHARecordedCallsMatchPython replays testdata/docker_ha_recorded.jsonl.gz:
// every call of the A09 public functions the whole Python test suite made
// (testdata/a09_record.py, PORTING rule 4). issue/enroll run against a
// temporary DATA_DIR rebuilt from the recorded files, with the recorded
// time.time() values, random bytes, datetime.now() and Agent list; the answer
// or exception, the files afterwards and the Agent list afterwards must match.
func TestDockerHARecordedCallsMatchPython(t *testing.T) {
	replayRecordFile(t, "testdata/docker_ha_recorded.jsonl.gz")
}

// TestDockerHAReviewCorpusMatchesPython replays testdata/docker_ha_review.jsonl.gz
// (testdata/gen_review_a09.py, the A09 review's CPython references for inputs
// the suite does not reach: shell metacharacters in every free-text guide value,
// damaged preflight outputs, hand-edited enrollment files and Agent lists).
func TestDockerHAReviewCorpusMatchesPython(t *testing.T) {
	replayRecordFile(t, "testdata/docker_ha_review.jsonl.gz")
}

func replayRecordFile(t *testing.T, name string) {
	file, err := os.Open(name)
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
	counts := map[string]int{}
	differ := 0
	for line := 0; scanner.Scan(); line++ {
		entry, err := parseJSONMap(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		fn := mapStr(entry, "fn")
		if mapList(entry, "patched") != nil {
			counts[fn+" patched"]++
			continue
		}
		// This row only tested opening the removed server lock path. O3-6
		// deliberately ignores that legacy directory; a boundary test below
		// proves enrollment succeeds and leaves it unchanged.
		if mapStr(entry, "test") == "review/issue/197" {
			counts[fn+" accepted (legacy lock ignored)"]++
			continue
		}
		outcome, problem := replayRecorded(t, entry)
		counts[fn+" "+outcome]++
		if problem != "" {
			differ++
			if differ <= 20 {
				t.Errorf("line %d %s (%s): %s", line+1, fn, mapStr(entry, "test"), problem)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%5d %s", counts[k], k)
	}
	t.Logf("recorded calls: %d differ", differ)
}

func canonical(v any) string {
	data, err := sortedCompactJSON(v)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return string(data)
}

func recordedDifference(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-80)
	return fmt.Sprintf("at byte %d: python …%q… go …%q…", i, a[lo:min(len(a), i+80)], b[lo:min(len(b), i+80)])
}

// digestRecordedBundle replaces got's compose_bundle.content_base64 by
// content_sha256 (sha256 of the zip bytes, moved to the end like the Python
// post-processing in gen_review_a10.py) when the recorded answer stores the
// digest instead of the archive.
func digestRecordedBundle(recorded, got any) any {
	want, isObject := recorded.(map[string]any)
	if !isObject || !mapHas(mapObj(want, "compose_bundle"), "content_sha256") {
		return got
	}
	answer, isObject := got.(map[string]any)
	if !isObject || !mapHas(mapObj(answer, "compose_bundle"), "content_base64") {
		return got
	}
	answer = cloneMap(answer)
	bundle := cloneMap(mapObj(answer, "compose_bundle"))
	raw, err := base64.StdEncoding.DecodeString(mapStr(bundle, "content_base64"))
	if err != nil {
		return got
	}
	sum := sha256.Sum256(raw)
	delete(bundle, "content_base64")
	bundle["content_sha256"] = hex.EncodeToString(sum[:])
	answer["compose_bundle"] = bundle
	return answer
}

// compareOutcome compares a Go answer/error with the recorded 'out'/'err'.
func compareOutcome(entry map[string]any, got any, err error) (string, string) {
	if errors.Is(err, ErrComposeVariantNotPorted) {
		return "skipped (A10)", ""
	}
	if err != nil && refusedByNetURL(entry["args"]) {
		return "accepted (N2: URL net/url refuses)", ""
	}
	if pyErr := mapList(entry, "err"); pyErr != nil {
		class, _ := pyErr[0].(string)
		message, _ := pyErr[1].(string)
		isValue, _ := pyErr[2].(bool)
		if err == nil {
			return "error", fmt.Sprintf("python raised %s %q, go answered %s", class, message, canonical(got))
		}
		var ee *EnrollmentError
		var ve *ValueError
		switch {
		case errors.As(err, &ee):
			status := -1
			if len(pyErr) > 3 {
				n, _ := asInt64(pyErr[3])
				status = int(n)
			}
			if class != "EnrollmentError" || message != ee.Message || status != ee.Status {
				return "error", fmt.Sprintf("python %s %q %d, go EnrollmentError %q %d", class, message, status, ee.Message, ee.Status)
			}
		case errors.As(err, &ve) || repo.IsValueError(err):
			if !isValue || class == "EnrollmentError" || message != err.Error() {
				return "error", fmt.Sprintf("python %s %q, go ValueError %q", class, message, err.Error())
			}
		default:
			if isValue {
				return "error", fmt.Sprintf("python ValueError %s %q, go other %v", class, message, err)
			}
		}
		return "error", ""
	}
	if err != nil {
		return "answer", fmt.Sprintf("python answered, go error %T %v", err, err)
	}
	expected := entry["out"]
	if value, present, oracleErr := writerExpected(mapStr(entry, "fn"), entry["args"]); oracleErr != nil {
		return "oracle", oracleErr.Error()
	} else if present {
		expected = value
	}
	want, have := canonical(expected), canonical(digestRecordedBundle(expected, got))
	if want != have && !nativeOutcomeEqual(expected, got, "") {
		return "answer", recordedDifference(want, have)
	}
	return "answer", ""
}

func replayRecorded(t *testing.T, entry map[string]any) (string, string) {
	args := mapList(entry, "args")
	switch mapStr(entry, "fn") {
	case "generate_guide":
		if request, isObject := args[0].(map[string]any); isObject {
			// Deliberate fix (coordinator decision 6): a server_url with one
			// trailing "\n" passes Python's re.match(r'^https?://[^\s]+$').
			if server, isString := mapGetOr(request, "server_url", nil).(string); isString && strings.HasSuffix(server, "\n") && isServerURL(strings.TrimSuffix(server, "\n")) {
				_, err := GenerateGuide(args[0], nil)
				var ve *ValueError
				if errors.As(err, &ve) && ve.Message == "请填写完整的 ConfigFlow HTTP(S) 地址" {
					return "deliberate (server_url + \"\\n\")", ""
				}
				return "deliberate", fmt.Sprintf("server_url %q: go %v", server, err)
			}
		}
		var enrollment *Enrollment
		if e, isObject := args[1].(map[string]any); isObject {
			url, urlOK := mapGetOr(e, "url", nil).(string)
			token, tokenOK := mapGetOr(e, "token", nil).(string)
			if !urlOK || !tokenOK {
				return "unsupported enrollment", ""
			}
			enrollment = &Enrollment{URL: url, Token: token, ExpiresAt: e["expires_at"]}
		} else if args[1] != nil {
			return "unsupported enrollment", ""
		}
		got, err := GenerateGuide(args[0], enrollment)
		if request, ok := args[0].(map[string]any); ok && got == nil && l1CompactColonRefusal(request, err) {
			return "deliberate (L1 D1 compact colon)", ""
		}
		if request, ok := args[0].(map[string]any); ok && got == nil && l1CompactPathWording(request, mapList(entry, "err"), err) {
			return "deliberate (L1 D1 path wording)", ""
		}
		// Deliberate fix (decision 6): an Agent name with one trailing "\n".
		if request, isObject := args[0].(map[string]any); isObject && mapHas(entry, "out") && deliberateAgentNameFix(request, err) {
			return "deliberate (agent_name + \"\\n\")", ""
		}
		return compareOutcome(entry, l1GuideBaseline(t, got), err)
	case "identity_command":
		got, err := IdentityCommand(args[0])
		return compareOutcome(entry, got, err)
	case "inspection_command":
		got, err := InspectionCommand(args[0])
		return compareOutcome(entry, got, err)
	case "collector_script":
		return compareOutcome(entry, CollectorScript(), nil)
	case "parse_report":
		got, err := ParsePreflightReport(args[0], args[1])
		if expected, present, problem := n810PreflightExpected(entry); problem != nil {
			return "oracle", problem.Error()
		} else if present {
			if err != nil || !testjson.Equal(expected, got) {
				return "native report", fmt.Sprintf("native report differs: %v", err)
			}
			return "approved native machine grammar", ""
		}
		return compareOutcome(entry, got, err)
	case "bootstrap_command":
		e, _ := args[0].(map[string]any)
		request, _ := args[1].(map[string]any)
		if e == nil || request == nil {
			return "unsupported", ""
		}
		got, err := BootstrapCommand(&Enrollment{URL: mapStr(e, "url"), Token: mapStr(e, "token")}, request)
		return compareOutcome(entry, got, err)
	case "issue", "enroll":
		return replayIO(t, entry)
	}
	return "unknown function", "no replay for " + mapStr(entry, "fn")
}

// recordedClock feeds the recorded time.time() values, random byte strings
// and datetime.now() values to the Go code.
type recordedClock struct {
	times   []any
	random  []any
	now     []any
	problem string
}

func (c *recordedClock) fail(format string, args ...any) {
	if c.problem == "" {
		c.problem = fmt.Sprintf(format, args...)
	}
}

func (c *recordedClock) install(t *testing.T) {
	savedNow, savedRandom, savedLocal := enrollmentNow, randomBytes, enrollmentLocalNow
	t.Cleanup(func() { enrollmentNow, randomBytes, enrollmentLocalNow = savedNow, savedRandom, savedLocal })
	enrollmentNow = func() float64 {
		if len(c.times) == 0 {
			c.fail("go read time.time() more often than python")
			return float64(time.Now().UnixNano()) / 1e9
		}
		value, _ := asFloat64(c.times[0])
		c.times = c.times[1:]
		return value
	}
	randomBytes = func(n int) ([]byte, error) {
		if len(c.random) == 0 {
			c.fail("go drew random bytes more often than python")
			return make([]byte, n), nil
		}
		data, _ := hex.DecodeString(c.random[0].(string))
		c.random = c.random[1:]
		if len(data) != n {
			c.fail("go drew %d random bytes, python %d", n, len(data))
		}
		return data, nil
	}
	enrollmentLocalNow = func() time.Time {
		if len(c.now) == 0 {
			c.fail("go read datetime.now() more often than python")
			return time.Now()
		}
		value, err := time.ParseInLocation("2006-01-02T15:04:05.999999", c.now[0].(string), time.Local)
		if err != nil {
			c.fail("datetime %v: %v", c.now[0], err)
		}
		c.now = c.now[1:]
		return value
	}
}

func (c *recordedClock) leftover() string {
	if c.problem != "" {
		return c.problem
	}
	if len(c.times) > 0 || len(c.random) > 0 || len(c.now) > 0 {
		return fmt.Sprintf("go used fewer values than python (left: %d times, %d random, %d now)", len(c.times), len(c.random), len(c.now))
	}
	return ""
}

// writeTree recreates a recorded docker-ha-installations tree under root.
func writeTree(t *testing.T, root string, tree map[string]any) {
	if tree == nil {
		return
	}
	if item := mapList(tree, ""); item != nil {
		// root itself is not a directory
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			t.Fatal(err)
		}
		if item[0] == "link" {
			if err := os.Symlink(item[2].(string), root); err != nil {
				t.Fatal(err)
			}
			return
		}
		data, _ := hex.DecodeString(item[2].(string))
		mode, _ := asInt64(item[1])
		if err := os.WriteFile(root, data, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	keys := mapKeys(tree)
	sort.Slice(keys, func(i, j int) bool {
		return strings.Count(keys[i], "/") < strings.Count(keys[j], "/") || strings.Count(keys[i], "/") == strings.Count(keys[j], "/") && keys[i] < keys[j]
	})
	var dirs []string
	modes := map[string]os.FileMode{}
	for _, rel := range keys {
		item := mapList(tree, rel)
		kind, _ := item[0].(string)
		mode, _ := asInt64(item[1])
		path := filepath.Join(root, rel)
		switch kind {
		case "dir":
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			dirs = append(dirs, path)
			modes[path] = os.FileMode(mode)
		case "file":
			data, _ := hex.DecodeString(item[2].(string))
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, os.FileMode(mode)); err != nil {
				t.Fatal(err)
			}
		case "link":
			if err := os.Symlink(item[2].(string), path); err != nil {
				t.Fatal(err)
			}
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err := os.Chmod(dirs[i], modes[dirs[i]]); err != nil {
			t.Fatal(err)
		}
	}
}

// readTree is a09_record._tree for the Go side.
func readTree(root string) any {
	info, err := os.Lstat(root)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(root)
		out[""] = []any{"link", int64(info.Mode().Perm()), target}
		return out
	}
	if !info.IsDir() {
		data, _ := os.ReadFile(root)
		out[""] = []any{"file", int64(info.Mode().Perm()), hex.EncodeToString(data)}
		return out
	}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		mode := int64(info.Mode().Perm())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			target, _ := os.Readlink(path)
			out[rel] = []any{"link", mode, target}
		case info.IsDir():
			out[rel] = []any{"dir", mode, ""}
		default:
			data, _ := os.ReadFile(path)
			out[rel] = []any{"file", mode, hex.EncodeToString(data)}
		}
		return nil
	})
	return out
}

// recordedManager is the Agent manager's _update_agents as recorded.
type recordedManager struct {
	record  map[string]any
	called  bool
	problem string
}

func (m *recordedManager) EditAgents(ctx context.Context, profileID string, edit func(context.Context, *[]model.Agent) error) error {
	m.called = true
	if m.record == nil {
		m.problem = "go called the Agent manager, python did not"
		return errors.New("unexpected manager call")
	}
	if want := m.record["profile_id"]; want != profileID {
		m.problem = fmt.Sprintf("profile id python %v, go %q", want, profileID)
	}
	before, ran := m.record["before"]
	if !ran {
		pyErr := mapList(m.record, "err")
		message, _ := pyErr[1].(string)
		if isValue, _ := pyErr[2].(bool); isValue {
			return &ValueError{Message: message}
		}
		return errors.New(message)
	}
	rawBefore, err := json.Marshal(before)
	if err != nil {
		return err
	}
	var agents []model.Agent
	if err := jsondecode.Unmarshal(rawBefore, &agents); err != nil {
		return err
	}
	err = edit(ctx, &agents)
	if pyErr := mapList(m.record, "updater_err"); pyErr != nil {
		if err == nil {
			m.problem = fmt.Sprintf("python updater raised %v, go returned nil", pyErr)
		}
		return err
	}
	if err != nil {
		m.problem = fmt.Sprintf("python updater succeeded, go: %v", err)
		return err
	}
	rawAfter, err := model.Marshal(agents)
	if err != nil {
		return err
	}
	afterAny, err := parseJSONAny(rawAfter)
	if err != nil {
		return err
	}
	if want, have := canonical(m.record["after"]), canonical(afterAny); want != have {
		m.problem = "agents after the update: " + recordedDifference(want, have)
	}
	if pyErr := mapList(m.record, "err"); pyErr != nil {
		message, _ := pyErr[1].(string)
		return errors.New(message)
	}
	return nil
}

func replayIO(t *testing.T, entry map[string]any) (string, string) {
	args := mapList(entry, "args")
	dataDir := filepath.Join(t.TempDir(), "data")
	if mapBool(entry, "data_dir_exists") {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	writeTree(t, root, mapObj(entry, "before"))
	clock := &recordedClock{times: mapList(entry, "times"), random: mapList(entry, "random"), now: mapList(entry, "now")}
	clock.install(t)
	beforeNative := canonical(readTree(root))
	var outcome, problem string
	switch mapStr(entry, "fn") {
	case "issue":
		body, isObject := args[0].(map[string]any)
		profileID, isString := args[1].(string)
		if !isObject || !isString {
			return "unsupported", ""
		}
		capability, err := IssueEnrollment(context.Background(), dataDir, body, profileID)
		if expected, present, problem := n810SessionExpected(entry); problem != nil {
			return "oracle", problem.Error()
		} else if present {
			return checkN810SessionRefusal(expected, err, capability == nil, beforeNative, root, false)
		}

		// Accepted difference: a hand-edited, unexpired session whose token is
		// not a string is echoed by Python; Go refuses it (500).
		if out, isObject := entry["out"].(map[string]any); isObject {
			if _, isString := mapGetOr(out, "token", nil).(string); !isString {
				var te *TypeError
				if errors.As(err, &te) {
					return "accepted (non-str session token)", clock.problem
				}
				return "accepted", fmt.Sprintf("non-str token: go %v", err)
			}
		}
		var got any
		if capability != nil {
			got = map[string]any{"url": capability.URL, "token": capability.Token, "expires_at": capability.ExpiresAt}
		}
		outcome, problem = compareOutcome(entry, got, err)
	case "enroll":
		sessionID, isString := args[0].(string)
		if !isString {
			return "unsupported", ""
		}
		token, present := args[1].(string)
		if args[1] != nil && !present {
			return "unsupported", ""
		}
		manager := &recordedManager{record: mapObj(entry, "manager")}
		got, err := Enroll(context.Background(), dataDir, manager, sessionID, token, present)
		if expected, present, problem := n810SessionExpected(entry); problem != nil {
			return "oracle", problem.Error()
		} else if present {
			return checkN810SessionRefusal(expected, err, got == nil, beforeNative, root, manager.called)
		}

		outcome, problem = compareOutcome(entry, got, err)
		if problem == "" {
			problem = manager.problem
		}
		if problem == "" && err == nil && !manager.called && mapObj(entry, "manager") != nil {
			problem = "python called the Agent manager, go did not"
		}
	}
	if problem == "" && mapHas(entry, "out") {
		problem = clock.leftover()
	}
	if problem == "" {
		wantTree, haveTree := entry["after"], readTree(root)
		// Python created .issue.lock; Go does not open or create it. All data
		// files and their metadata are still compared, without regenerating corpus.
		if tree, ok := wantTree.(map[string]any); ok {
			tree = cloneMap(tree)
			delete(tree, ".issue.lock")
			wantTree = tree
		}
		if tree, ok := haveTree.(map[string]any); ok {
			delete(tree, ".issue.lock")
		}
		if want, have := canonical(n810SessionTreeJSON(wantTree)), canonical(n810SessionTreeJSON(haveTree)); want != have {
			problem = "files afterwards: " + recordedDifference(want, have)
		}
	}
	return outcome, problem
}

func checkN810SessionRefusal(expected map[string]any, err error, empty bool, before, root string, called bool) (string, string) {
	var refusal *EnrollmentError
	var malformed *RenderError
	status := int(mapInt(expected, "status"))
	valid := errors.As(err, &refusal) && refusal.Status == status && refusal.Message == mapStr(expected, "message")
	if status == 500 {
		valid = errors.As(err, &malformed) && malformed.Message == mapStr(expected, "message")
		if mapStr(expected, "class") == "model-type" {
			valid = errors.Is(err, model.ErrType)
		}
	}
	if !valid {
		return "native session", fmt.Sprintf("wrong refusal: %v", err)
	}
	if !empty || called != mapBool(expected, "registry") || canonical(readTree(root)) != before {
		return "native session", "refusal returned credentials, called registry or wrote files"
	}
	return "approved exact session-ID refusal", ""
}
func n810SessionTreeJSON(value any) any {
	tree, ok := value.(map[string]any)
	if !ok {
		return value
	}
	result := cloneMap(tree)
	for name, item := range result {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		entry, ok := item.([]any)
		if !ok || len(entry) != 3 || entry[0] != "file" {
			continue
		}
		text, ok := entry[2].(string)
		if !ok {
			continue
		}
		raw, err := hex.DecodeString(text)
		if err != nil {
			continue
		}
		decoded, err := parseJSONAny(raw)
		if err != nil {
			continue
		}
		entry[2] = decoded
	}
	return result
}
