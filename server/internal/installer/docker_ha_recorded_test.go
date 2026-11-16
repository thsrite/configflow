package installer

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
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
		value, err := jsonx.Parse(scanner.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		entry := value.(*jsonx.Object)
		fn := entry.GetString("fn")
		if entry.GetList("patched") != nil {
			counts[fn+" patched"]++
			continue
		}
		outcome, problem := replayRecorded(t, entry)
		counts[fn+" "+outcome]++
		if problem != "" {
			differ++
			if differ <= 20 {
				t.Errorf("line %d %s (%s): %s", line+1, fn, entry.GetString("test"), problem)
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
	data, err := jsonx.Encode(v, jsonx.Options{SortKeys: true, Compact: true})
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

// compareOutcome compares a Go answer/error with the recorded 'out'/'err'.
func compareOutcome(entry *jsonx.Object, got any, err error) (string, string) {
	if errors.Is(err, ErrComposeVariantNotPorted) {
		return "skipped (A10)", ""
	}
	if pyErr := entry.GetList("err"); pyErr != nil {
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
				n, _ := jsonx.AsInt(pyErr[3])
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
	want, have := canonical(entry.Value("out")), canonical(got)
	if want != have {
		return "answer", recordedDifference(want, have)
	}
	return "answer", ""
}

func replayRecorded(t *testing.T, entry *jsonx.Object) (string, string) {
	args := entry.GetList("args")
	switch entry.GetString("fn") {
	case "generate_guide":
		if request, isObject := args[0].(*jsonx.Object); isObject {
			// Deliberate fix (coordinator decision 6): a server_url with one
			// trailing "\n" passes Python's re.match(r'^https?://[^\s]+$').
			if server, isString := request.GetOr("server_url", nil).(string); isString && strings.HasSuffix(server, "\n") && isServerURL(strings.TrimSuffix(server, "\n")) {
				_, err := GenerateGuide(args[0], nil)
				var ve *ValueError
				if errors.As(err, &ve) && ve.Message == "请填写完整的 ConfigFlow HTTP(S) 地址" {
					return "deliberate (server_url + \"\\n\")", ""
				}
				return "deliberate", fmt.Sprintf("server_url %q: go %v", server, err)
			}
		}
		var enrollment *Enrollment
		if e, isObject := args[1].(*jsonx.Object); isObject {
			url, urlOK := e.GetOr("url", nil).(string)
			token, tokenOK := e.GetOr("token", nil).(string)
			if !urlOK || !tokenOK {
				return "unsupported enrollment", ""
			}
			enrollment = &Enrollment{URL: url, Token: token, ExpiresAt: e.Value("expires_at")}
		} else if args[1] != nil {
			return "unsupported enrollment", ""
		}
		got, err := GenerateGuide(args[0], enrollment)
		return compareOutcome(entry, got, err)
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
		return compareOutcome(entry, got, err)
	case "bootstrap_command":
		e, _ := args[0].(*jsonx.Object)
		request, _ := args[1].(*jsonx.Object)
		if e == nil || request == nil {
			return "unsupported", ""
		}
		got, err := BootstrapCommand(&Enrollment{URL: e.GetString("url"), Token: e.GetString("token")}, request)
		return compareOutcome(entry, got, err)
	case "issue", "enroll":
		return replayIO(t, entry)
	}
	return "unknown function", "no replay for " + entry.GetString("fn")
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
		value, _ := jsonx.AsFloat(c.times[0])
		c.times = c.times[1:]
		return value
	}
	randomBytes = func(n int) []byte {
		if len(c.random) == 0 {
			c.fail("go drew random bytes more often than python")
			return make([]byte, n)
		}
		data, _ := hex.DecodeString(c.random[0].(string))
		c.random = c.random[1:]
		if len(data) != n {
			c.fail("go drew %d random bytes, python %d", n, len(data))
		}
		return data
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
func writeTree(t *testing.T, root string, tree *jsonx.Object) {
	if tree == nil {
		return
	}
	if item := tree.GetList(""); item != nil {
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
		mode, _ := jsonx.AsInt(item[1])
		if err := os.WriteFile(root, data, os.FileMode(mode)); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	keys := tree.Keys()
	sort.Slice(keys, func(i, j int) bool {
		return strings.Count(keys[i], "/") < strings.Count(keys[j], "/") || strings.Count(keys[i], "/") == strings.Count(keys[j], "/") && keys[i] < keys[j]
	})
	var dirs []string
	modes := map[string]os.FileMode{}
	for _, rel := range keys {
		item := tree.GetList(rel)
		kind, _ := item[0].(string)
		mode, _ := jsonx.AsInt(item[1])
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
	out := jsonx.NewObject()
	if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(root)
		out.Set("", []any{"link", jsonx.Int(int64(info.Mode().Perm())), target})
		return out
	}
	if !info.IsDir() {
		data, _ := os.ReadFile(root)
		out.Set("", []any{"file", jsonx.Int(int64(info.Mode().Perm())), hex.EncodeToString(data)})
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
			out.Set(rel, []any{"link", jsonx.Int(mode), target})
		case info.IsDir():
			out.Set(rel, []any{"dir", jsonx.Int(mode), ""})
		default:
			data, _ := os.ReadFile(path)
			out.Set(rel, []any{"file", jsonx.Int(mode), hex.EncodeToString(data)})
		}
		return nil
	})
	return out
}

// recordedManager is the Agent manager's _update_agents as recorded.
type recordedManager struct {
	record  *jsonx.Object
	called  bool
	problem string
}

func (m *recordedManager) UpdateAgents(ctx context.Context, profileID string, updater func(context.Context, *[]any) (any, error)) (any, error) {
	m.called = true
	if m.record == nil {
		m.problem = "go called the Agent manager, python did not"
		return nil, errors.New("unexpected manager call")
	}
	if want := m.record.Value("profile_id"); want != profileID {
		m.problem = fmt.Sprintf("profile id python %v, go %q", want, profileID)
	}
	before, ran := m.record.Get("before")
	if !ran {
		pyErr := m.record.GetList("err")
		message, _ := pyErr[1].(string)
		if isValue, _ := pyErr[2].(bool); isValue {
			return nil, &ValueError{Message: message}
		}
		return nil, errors.New(message)
	}
	agents := jsonx.DeepCopy(before).([]any)
	value, err := updater(ctx, &agents)
	if pyErr := m.record.GetList("updater_err"); pyErr != nil {
		if err == nil {
			m.problem = fmt.Sprintf("python updater raised %v, go returned %s", pyErr, canonical(value))
		}
		return value, err
	}
	if err != nil {
		m.problem = fmt.Sprintf("python updater succeeded, go: %v", err)
		return value, err
	}
	if want, have := canonical(m.record.Value("after")), canonical(agents); want != have {
		m.problem = "agents after the update: " + recordedDifference(want, have)
	}
	if pyErr := m.record.GetList("err"); pyErr != nil {
		message, _ := pyErr[1].(string)
		return nil, errors.New(message)
	}
	return value, nil
}

func replayIO(t *testing.T, entry *jsonx.Object) (string, string) {
	args := entry.GetList("args")
	dataDir := filepath.Join(t.TempDir(), "data")
	if entry.GetBool("data_dir_exists") {
		if err := os.MkdirAll(dataDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	root := filepath.Join(dataDir, "docker-ha-installations")
	writeTree(t, root, entry.GetObject("before"))
	clock := &recordedClock{times: entry.GetList("times"), random: entry.GetList("random"), now: entry.GetList("now")}
	clock.install(t)
	var outcome, problem string
	switch entry.GetString("fn") {
	case "issue":
		body, isObject := args[0].(*jsonx.Object)
		profileID, isString := args[1].(string)
		if !isObject || !isString {
			return "unsupported", ""
		}
		capability, err := IssueEnrollment(dataDir, body, profileID)
		// Accepted difference: a hand-edited, unexpired session whose token is
		// not a string is echoed by Python; Go refuses it (500).
		if out, isObject := entry.Value("out").(*jsonx.Object); isObject {
			if _, isString := out.GetOr("token", nil).(string); !isString {
				var te *TypeError
				if errors.As(err, &te) {
					return "accepted (non-str session token)", clock.problem
				}
				return "accepted", fmt.Sprintf("non-str token: go %v", err)
			}
		}
		var got any
		if capability != nil {
			got = jsonx.Obj("url", capability.URL, "token", capability.Token, "expires_at", capability.ExpiresAt)
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
		manager := &recordedManager{record: entry.GetObject("manager")}
		got, err := Enroll(context.Background(), dataDir, manager, sessionID, token, present)
		outcome, problem = compareOutcome(entry, got, err)
		if problem == "" {
			problem = manager.problem
		}
		if problem == "" && !manager.called && entry.GetObject("manager") != nil {
			problem = "python called the Agent manager, go did not"
		}
	}
	if problem == "" {
		problem = clock.leftover()
	}
	if problem == "" {
		if want, have := canonical(entry.Value("after")), canonical(readTree(root)); want != have {
			problem = "files afterwards: " + recordedDifference(want, have)
		}
	}
	return outcome, problem
}
