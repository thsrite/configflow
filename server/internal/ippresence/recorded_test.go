package ippresence

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/repo"
	"configflow/internal/rulesetfmt"
	"configflow/internal/testjson"
	"configflow/internal/testjson/modeldirt"
)

// Replay of every rule_ip_presence.classical_ip_lines call the Python suite
// made (routersplit/testdata/r02_record.py -> r02_recorded.jsonl.gz). Each
// row gets a fresh repository whose stored rule library and rules directory
// are the snapshot taken when Python made the call; the memo is cleared
// first, as the recorder cleared Python's.

func encodeJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%v>", err)
	}
	return string(b)
}

func TestRecordedPythonTests(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_recorded.jsonl.gz")
}

// TestCorpus replays the classical_ip_lines rows of the random corpus
// (routersplit/testdata/gen_r02.py): random libraries, declarations,
// inline and cached contents (payload YAML, BOM, invalid UTF-8, line
// separators), mrs formats, legacy cache names.
func TestCorpus(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_corpus.jsonl.gz")
}

type recordedRow struct {
	Fn    string          `json:"fn"`
	Test  string          `json:"test"`
	Args  recordedArgs    `json:"args"`
	Rules recordedRules   `json:"rules"`
	Out   json.RawMessage `json:"out"`
	Err   *recordedError  `json:"err"`
}

type recordedArgs struct {
	Item   json.RawMessage `json:"item"`
	Config json.RawMessage `json:"config"`
}

type recordedRules struct {
	RootError    json.RawMessage   `json:"root_error"`
	LibraryError json.RawMessage   `json:"library_error"`
	Library      json.RawMessage   `json:"library"`
	Files        map[string]string `json:"files"`
}

type recordedError struct {
	Class string `json:"class"`
	Msg   string `json:"msg"`
}

type nativePresenceRow struct {
	File         string         `json:"file"`
	Test         string         `json:"test"`
	SHA256       string         `json:"sha256"`
	PythonError  *recordedError `json:"python_error"`
	NativeOutput *bool          `json:"native_output"`
}

func decodeTypedArgs(itemRaw, configRaw json.RawMessage) (rule model.RuleConfig, library []model.RuleLibraryItem, ok bool, unknown bool) {
	itemTrim := bytes.TrimSpace(itemRaw)
	if len(itemTrim) == 0 || itemTrim[0] != '{' {
		return rule, nil, false, false
	}
	if err := json.Unmarshal(itemTrim, &rule); err != nil {
		return rule, nil, false, false
	}
	// Check if item needs config (non-inline or has library_rule_id).
	var itemFields map[string]json.RawMessage
	_ = json.Unmarshal(itemTrim, &itemFields)
	if libID, present := itemFields["library_rule_id"]; present && !bytes.Equal(bytes.TrimSpace(libID), []byte("null")) {
		cfgTrim := bytes.TrimSpace(configRaw)
		if len(cfgTrim) == 0 || cfgTrim[0] != '{' {
			return rule, nil, false, false
		}
	}
	srcType := "url"
	if rule.SourceType != nil {
		srcType = *rule.SourceType
	}
	cfgTrim := bytes.TrimSpace(configRaw)
	if len(cfgTrim) == 0 || bytes.Equal(cfgTrim, []byte("null")) {
		if srcType == "content" {
			return rule, nil, true, false
		}
		if rule.URL == nil || *rule.URL == "" {
			return rule, nil, true, false
		}
		return rule, nil, false, false
	}
	if cfgTrim[0] != '{' {
		if srcType == "content" {
			return rule, nil, true, false
		}
		if rule.URL == nil || *rule.URL == "" {
			return rule, nil, true, false
		}
		return rule, nil, false, false
	}
	var cfgFields map[string]json.RawMessage
	if err := json.Unmarshal(cfgTrim, &cfgFields); err != nil {
		return rule, nil, false, false
	}
	libRaw, present := cfgFields["rule_library"]
	if !present {
		return rule, nil, true, false
	}
	libTrim := bytes.TrimSpace(libRaw)
	if bytes.Equal(libTrim, []byte("null")) {
		return rule, nil, true, false
	}
	switch libTrim[0] {
	case '[':
		var entries []json.RawMessage
		if err := json.Unmarshal(libTrim, &entries); err != nil {
			return rule, nil, false, false
		}
		for _, entry := range entries {
			trimmed := bytes.TrimSpace(entry)
			if len(trimmed) == 0 || trimmed[0] != '{' {
				match, err := rulesetfmt.LibraryIndex(rule, library)
				if err != nil || match < 0 {
					return rule, nil, true, true
				}
				return rule, library, true, false
			}
			var libItem model.RuleLibraryItem
			if err := json.Unmarshal(trimmed, &libItem); err != nil {
				return rule, nil, true, true
			}
			library = append(library, libItem)
		}
		return rule, library, true, false
	case '"':
		if bytes.Equal(libTrim, []byte(`""`)) {
			return rule, []model.RuleLibraryItem{}, true, false
		}
		return rule, nil, false, false
	case '{':
		var empty map[string]json.RawMessage
		if json.Unmarshal(libTrim, &empty) == nil && len(empty) == 0 {
			return rule, []model.RuleLibraryItem{}, true, false
		}
		return rule, nil, false, false
	default:
		return rule, nil, false, false
	}
}

func replay(t *testing.T, path string) {
	fixture := newReplayRepository(t)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(gz)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	identical, differ, notExpressible, untypedExcluded := 0, 0, 0, 0
	accepted := map[string]int{}
	native, applied := nativePresenceExpectations(t, filepath.Base(path)), 0
	cacheReferences := presenceCacheReferences(t, path)
	usedCaches := 0
	tests, files := map[string]bool{}, map[string]bool{}
	outcomes := map[string]int{}
	for scanner.Scan() {
		lineBytes := scanner.Bytes()
		var row recordedRow
		if err := json.Unmarshal(lineBytes, &row); err != nil {
			t.Fatal(err)
		}
		if row.Fn != "classical_ip_lines" {
			continue
		}
		if len(row.Rules.RootError) > 0 || len(row.Rules.LibraryError) > 0 {
			notExpressible++
			continue
		}
		tests[row.Test] = true
		files[strings.SplitN(row.Test, "::", 2)[0]] = true
		ctx, problem := fixture.reset(t, row.Rules)
		if problem != "" {
			t.Errorf("%s: %s", row.Test, problem)
			differ++
			continue
		}
		ClearMemo()
		rule, library, typedOK, unknown := decodeTypedArgs(row.Args.Item, row.Args.Config)
		dirt := presenceRowDirt(row.Args.Item, row.Args.Config)
		if !typedOK {
			if row.Err == nil && (len(bytes.TrimSpace(row.Out)) == 0 || bytes.Equal(bytes.TrimSpace(row.Out), []byte("null"))) {
				untypedExcluded++
				continue
			}
			if row.Err != nil && (row.Err.Class == "AttributeError" || row.Err.Class == "TypeError") {
				if nf := native[row.Test]; nf != nil {
					applied++
				}
				untypedExcluded++
				continue
			}
		}
		var out *bool
		var callErr error
		if !unknown {
			out, callErr = TypedClassicalIPLines(ctx, rule, library)
		}
		sum := fmt.Sprintf("%x", sha256.Sum256(lineBytes))
		if ref, ok := cacheReferences[sum]; ok {
			wantOut, _ := testjson.Parse(row.Out)
			if ref.Kind != "D2 unowned legacy cache" || ref.NativeOutput != nil || !testjson.Equal(wantOut, ref.PythonOutput) || row.Err != nil {
				t.Fatal("unowned cache reference lost its exact old outcome")
			}
			if out != nil || callErr != nil {
				t.Errorf("unowned legacy cache must be unknown: %v %v", out, callErr)
				differ++
			} else {
				identical++
			}
			usedCaches++
			continue
		}
		if nf := native[row.Test]; nf != nil {
			if sum != nf.SHA256 || nf.PythonError == nil || row.Err == nil || nf.PythonError.Class != row.Err.Class || nf.PythonError.Msg != row.Err.Msg {
				t.Fatalf("native presence row %s changed", row.Test)
			}
			if out != nil || callErr != nil || nf.NativeOutput != nil {
				t.Errorf("native presence row %s: want unknown, got %v / %v", row.Test, out, callErr)
				differ++
			} else {
				identical++
			}
			applied++
			continue
		}
		var problems []string
		if row.Err != nil {
			if row.Test == "corpus::classical_ip_lines::1589" {
				// N7-3c §5.7: this one scalar-payload refusal uses the native
				// validator, without reproducing Python's internal TypeError.
				if row.Err.Class != "TypeError" || callErr == nil || callErr.Error() != mosdns.PayloadCollectionMessage {
					problems = append(problems, fmt.Sprintf("native scalar-payload refusal: %v", callErr))
				}
				outcomes["native scalar-payload refusal"]++
			} else if row.Err.Class == "AttributeError" || row.Err.Class == "TypeError" {
				// Typed model records cannot represent non-dict/non-string dynamic Python errors;
				// unknown (nil, nil) is the typed boundary behaviour.
				if out != nil || callErr != nil {
					problems = append(problems, fmt.Sprintf("python raised %s, go %v / %v", encodeJSON(row.Err), out, callErr))
				}
				outcomes["typed-unknown "+row.Err.Class]++
			} else {
				problems = append(problems, fmt.Sprintf("python raised %s, go %v", encodeJSON(row.Err), callErr))
			}
		} else if callErr != nil {
			problems = append(problems, "go error "+callErr.Error())
		} else {
			gotEnc := "null"
			if out != nil {
				gotEnc = encodeJSON(*out)
			}
			wantVal, _ := testjson.Parse(row.Out)
			gotVal, _ := testjson.Parse([]byte(gotEnc))
			if !testjson.Equal(wantVal, gotVal) {
				problems = append(problems, fmt.Sprintf("python %s go %s", string(row.Out), gotEnc))
			} else {
				outcomes[gotEnc]++
			}
		}
		if len(problems) > 0 && len(dirt) > 0 {
			// Accepted O18 T1c difference: the item or a library entry holds
			// dirty stored data that plain records read differently from
			// Python on purpose (a number in a text member such as
			// library_rule_id or content reads as its literal, null and ""
			// read as missing, undeclared members are dropped).
			accepted[strings.Join(dirt, "+")]++
			continue
		}
		if len(problems) > 0 {
			differ++
			if differ <= 20 {
				t.Errorf("%s classical_ip_lines(%s): %s", row.Test, string(row.Args.Item), strings.Join(problems, "; "))
			}
			continue
		}
		identical++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if usedCaches != len(cacheReferences) {
		t.Fatalf("unused exact cache references: %d/%d", usedCaches, len(cacheReferences))
	}
	if applied != len(native) {
		t.Fatalf("applied %d of %d native presence rows", applied, len(native))
	}
	t.Logf("%s classical_ip_lines: %d identical, %d differ, %d untyped-excluded, %d not expressible, accepted O18 T1c %v; from %d tests in %d files; outcomes %v",
		path, identical, differ, untypedExcluded, notExpressible, accepted, len(tests), len(files), outcomes)
}

// presenceRowDirt returns the O18 T1c dirt kinds of a recorded call: those of
// the rule item and of the configuration's rule library entries.
func presenceRowDirt(item, config json.RawMessage) []string {
	kinds := map[string]bool{}
	for _, k := range modeldirt.Of(item, model.RuleConfig{}) {
		kinds[k] = true
	}
	var cfg struct {
		Library json.RawMessage `json:"rule_library"`
	}
	if json.Unmarshal(config, &cfg) == nil && len(cfg.Library) > 0 {
		for _, k := range modeldirt.Of(cfg.Library, []model.RuleLibraryItem{}) {
			kinds[k] = true
		}
	}
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func nativePresenceExpectations(t *testing.T, file string) map[string]*nativePresenceRow {
	t.Helper()
	data, err := os.ReadFile("testdata/n88a-native.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []nativePresenceRow
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatal(err)
	}
	result := map[string]*nativePresenceRow{}
	for i := range entries {
		entry := &entries[i]
		if entry.File == file {
			if result[entry.Test] != nil || entry.PythonError == nil || entry.PythonError.Class != "AttributeError" {
				t.Fatalf("invalid native presence fixture %s", entry.Test)
			}
			result[entry.Test] = entry
		}
	}
	counts := map[string]int{"r02_corpus.jsonl.gz": 14, "r02_edge.jsonl.gz": 8}
	if len(result) != counts[file] {
		t.Fatalf("native presence fixture count for %s changed", file)
	}
	return result
}

// replayRepository shares only schema/default initialization. Each recorded
// call replaces the library and cache files, and replay clears the content memo.
// Reusing this fixture must not let a previous row supply undeclared input.
type replayRepository struct {
	r       *repo.Repository
	ctx     context.Context
	root    string
	library string
}

func newReplayRepository(t *testing.T) *replayRepository {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	return &replayRepository{r: r, ctx: repo.WithRepository(context.Background(), r), root: root}
}

func (f *replayRepository) reset(t *testing.T, rules recordedRules) (context.Context, string) {
	t.Helper()
	libRaw := bytes.TrimSpace(rules.Library)
	if len(libRaw) == 0 || bytes.Equal(libRaw, []byte("null")) {
		libRaw = []byte("[]")
	}
	encoded := string(libRaw)
	if encoded != f.library {
		var shared model.Shared
		if err := json.Unmarshal([]byte(`{"rule_library":`+encoded+`}`), &shared); err != nil {
			return nil, "cannot decode recorded rule library: " + err.Error()
		}
		if err := f.r.UpdateShared(f.ctx, func(_ context.Context, current *model.Shared) error {
			current.RuleLibrary = shared.RuleLibrary
			return nil
		}); err != nil {
			return nil, "cannot store the recorded rule library: " + err.Error()
		}
		f.library = encoded
	}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(f.root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range rules.Files {
		raw, err := hex.DecodeString(data)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(f.root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return f.ctx, ""
}

// TestEdgeCorpus replays the classical_ip_lines rows of the hand-picked edge
// corpus (routersplit/testdata/gen_r02_edge.py, review R02): legacy-name
// cache path with malformed library entries, by-id caches, library_rule_id
// shapes, url/source_type shapes, Unicode contents and invalid UTF-8.
func TestEdgeCorpus(t *testing.T) {
	t.Parallel()
	replay(t, "../routersplit/testdata/r02_edge.jsonl.gz")
}

// TestReplayRepositoryDoesNotLeakInputs guards the shared fixture reset itself.
// A missing cache-directory reset leaves the first row's extra file behind.
func TestReplayRepositoryDoesNotLeakInputs(t *testing.T) {
	f := newReplayRepository(t)
	first := recordedRules{
		Library: json.RawMessage(`[{"id":"old","name":"old"}]`),
		Files:   map[string]string{"old.list": hex.EncodeToString([]byte("192.0.2.0/24\n"))},
	}
	if _, problem := f.reset(t, first); problem != "" {
		t.Fatal(problem)
	}
	if _, err := os.Stat(filepath.Join(f.root, "old.list")); err != nil {
		t.Fatal(err)
	}
	second := recordedRules{
		Library: json.RawMessage(`[]`),
		Files:   map[string]string{},
	}
	if _, problem := f.reset(t, second); problem != "" {
		t.Fatal(problem)
	}
	shared, err := f.r.Shared(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared.RuleLibrary) != 0 {
		t.Fatal("previous row library leaked")
	}
	files, err := os.ReadDir(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("previous row files leaked", files)
	}
}
