package mosdns

// Replay of the C28 calls recorded while the whole Python test suite ran
// (testdata/c28_record.py): build_mosdns_zip, mosdns_rule_proxy,
// _fetch_remote_content, _local_content_rule, _load_cached_rule_content_for_url,
// _cache_settings, _path, _require_rule_proxy_auth, _validate_remote_url.
// Comparison is functional: outputs exactly; errors by "rejected", the
// Python class family and — for the user-facing errors (MosdnsArchiveError,
// RemoteRuleValidationError) — the Chinese message byte for byte.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/taskstatus"
)

func recordedBytes(v any) ([]byte, bool) {
	if o, ok := v.(*jsonx.Object); ok {
		if text, ok := o.Value("__bytes__").(string); ok {
			data, _ := hex.DecodeString(text)
			return data, true
		}
	}
	return nil, false
}

func recordedValue(v any) any {
	if data, ok := recordedBytes(v); ok {
		return data
	}
	return v
}

func valueForCompare(v any) any {
	if data, ok := v.([]byte); ok {
		return jsonx.Obj("__bytes__", hex.EncodeToString(data))
	}
	return v
}

// buildRecordedError turns a recorded exception into the Go error the
// collaborator would return.
func buildRecordedError(spec *jsonx.Object) error {
	class, message := spec.GetString("class"), spec.GetString("message")
	status := int(spec.GetInt("status"))
	switch {
	case spec.Value("requests") == true:
		return &rulefetch.FetchError{Class: class, Message: message, Status: status}
	case class == "RemoteRuleValidationError":
		return rulefetch.NewRemoteRuleMessage(message)
	case class == "MosdnsArchiveError":
		return &ArchiveError{Message: message}
	case class == "ProfileValidationError":
		return repo.NewValidationError(message)
	}
	return &convert.PyError{Class: class, Message: message}
}

// userFacing errors carry a message the user sees verbatim.
var userFacing = map[string]bool{"MosdnsArchiveError": true, "RemoteRuleValidationError": true}

func compareError(t *testing.T, label string, got error, want *jsonx.Object) bool {
	t.Helper()
	if got == nil {
		t.Errorf("%s: no error, want %s %q", label, want.GetString("class"), want.GetString("message"))
		return false
	}
	class := want.GetString("class")
	if userFacing[class] {
		if pythonClass(got) != class || got.Error() != want.GetString("message") {
			t.Errorf("%s:\n  got  %s %q\n  want %s %q", label, pythonClass(got), got.Error(), class, want.GetString("message"))
			return false
		}
		return true
	}
	if want.Value("value_error") == true && !isValueError(got) {
		t.Errorf("%s: got %s %q, want a ValueError (%s %q)", label, pythonClass(got), got.Error(), class, want.GetString("message"))
		return false
	}
	return true
}

type replayWorld struct {
	ctx context.Context
	r   *repo.Repository
}

// seedWorld installs the recorded repository state: the shared rule
// library, the rule cache files and task-status.json. ok=false when the
// state cannot be reproduced on a fresh repository.
func seedWorld(t *testing.T, world *jsonx.Object) (replayWorld, bool) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	if world == nil {
		return replayWorld{ctx, r}, true
	}
	if id := world.GetString("default_profile"); id != "" && id != "default" {
		return replayWorld{}, false
	}
	if library := world.GetList("rule_library"); len(library) > 0 {
		if _, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			shared.Set("rule_library", jsonx.DeepCopy(library))
			return nil, nil
		}); err != nil {
			return replayWorld{}, false
		}
	}
	rulesDir, err := r.SharedRulesDir()
	if err != nil {
		t.Fatal(err)
	}
	for label, value := range world.GetObject("files").All() {
		data, ok := recordedBytes(value)
		if !ok {
			continue
		}
		path := filepath.Join(rulesDir, strings.TrimPrefix(label, "rules/"))
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if text, ok := world.Value("task_status").(string); ok {
		os.WriteFile(filepath.Join(r.DataDir(), taskstatus.Filename), []byte(text), 0o644)
	}
	return replayWorld{ctx, r}, true
}

func ruleFiles(t *testing.T, r *repo.Repository) *jsonx.Object {
	rulesDir, _ := r.SharedRulesDir()
	out := jsonx.NewObject()
	entries, _ := os.ReadDir(rulesDir)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || strings.Contains(name, ".tmp") || strings.HasSuffix(name, ".lock") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(rulesDir, name))
		out.Set("rules/"+name, jsonx.Obj("__bytes__", hex.EncodeToString(data)))
	}
	return out
}

// fetchAnswers replays _fetch_remote_content answers keyed by (url, raw).
type fetchAnswers struct {
	mu      sync.Mutex
	answers map[string][]*jsonx.Object
	missing int
}

func newFetchAnswers(list []any) *fetchAnswers {
	f := &fetchAnswers{answers: map[string][]*jsonx.Object{}}
	for _, item := range list {
		answer := item.(*jsonx.Object)
		args := answer.GetList("args")
		key := jsonx.Str(args[0]) + "\x00" + jsonx.Str(args[2])
		f.answers[key] = append(f.answers[key], answer)
	}
	return f
}

func (f *fetchAnswers) fetch(_ context.Context, url string, _ *jsonx.Object, raw bool, _ string) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := url + "\x00" + jsonx.Str(raw)
	queue := f.answers[key]
	if len(queue) == 0 {
		f.missing++
		return nil, &rulefetch.FetchError{Class: "ConnectionError", Message: "unrecorded fetch " + url}
	}
	answer := queue[0]
	if len(queue) > 1 {
		f.answers[key] = queue[1:]
	}
	if spec := answer.GetObject("err"); spec != nil {
		return nil, buildRecordedError(spec)
	}
	return recordedValue(answer.Value("ok")), nil
}

func firstAnswer(answers *jsonx.Object, key string) *jsonx.Object {
	list := answers.GetList(key)
	if len(list) == 0 {
		return nil
	}
	return list[0].(*jsonx.Object)
}

func TestRecordedC28PythonSuite(t *testing.T) {
	rows := loadRows(t, "testdata/c28_recorded.jsonl.gz")
	counts := map[string]int{}
	skipped := map[string]int{}
	failures := 0
	for i, row := range rows {
		fn := row.GetString("fn")
		label := fn + " #" + jsonx.Str(i) + " " + row.GetString("test")
		if row.Has("unsupported") {
			skipped["unsupported: "+row.GetString("unsupported")]++
			continue
		}
		ok := replayRow(t, label, row, skipped)
		if !ok {
			failures++
		}
		counts[fn]++
	}
	if failures > 0 {
		t.Errorf("%d of %d recorded calls differ", failures, len(rows))
	}
	t.Logf("replayed %v; skipped %v", counts, skipped)
}

func replayRow(t *testing.T, label string, row *jsonx.Object, skipped map[string]int) bool {
	t.Helper()
	fn := row.GetString("fn")
	args := row.GetList("args")
	want := row.Value("out")
	wantErr := row.GetObject("err")
	check := func(got any, err error) bool {
		if wantErr != nil {
			return compareError(t, label, err, wantErr)
		}
		if err != nil {
			t.Errorf("%s: unexpected error %s %v", label, pythonClass(err), err)
			return false
		}
		if compact(valueForCompare(got)) != compact(want) {
			t.Errorf("%s:\n  got  %.600s\n  want %.600s", label, compact(valueForCompare(got)), compact(want))
			return false
		}
		return true
	}
	switch fn {
	case "path":
		out, err := ArchivePath(args[0])
		return check(out, err)
	case "cache_settings":
		out, err := CacheSettings(args[0])
		return check(out, err)
	case "validate_url":
		out, err := ValidateRemoteURL(args[0].(string))
		return check(out, err)
	case "local_content":
		config, _ := args[1].(*jsonx.Object)
		content, found, err := LocalContentRule(args[0].(string), config, args[2].(string))
		if !found && err == nil {
			content = nil
		}
		return check(content, err)
	case "cached_url":
		world, ok := seedWorld(t, row.GetObject("world"))
		if !ok {
			skipped["world"]++
			return true
		}
		out, err := LoadCachedRuleContentForURL(world.ctx, args[0].(string), args[1].(*jsonx.Object))
		return check(out, err)
	case "auth":
		return replayAuth(t, label, row, check)
	case "fetch_remote":
		return replayFetchRemote(t, label, row, check)
	case "build":
		return replayBuild(t, label, row, check)
	case "proxy":
		return replayProxy(t, label, row, skipped)
	}
	t.Errorf("%s: unknown fn", label)
	return false
}

func replayAuth(t *testing.T, label string, row *jsonx.Object, check func(any, error) bool) bool {
	args := row.GetObject("args")
	tokens := repo.TokenSet{}
	for _, token := range args.GetList("internal_tokens") {
		tokens[token.(string)] = struct{}{}
	}
	header, present := args.Value("authorization").(string)
	token, _ := args.Value("token").(string)
	verified := row.GetList("verified")
	next := 0
	verify := func(string) (auth.Verification, error) {
		if next >= len(verified) {
			return auth.Verification{Error: "invalid"}, nil
		}
		payload, _ := verified[next].(*jsonx.Object)
		next++
		return auth.Verification{Claims: payload}, nil
	}
	config := jsonx.Obj("system_config", args.Value("system_config"))
	out, err := RuleProxyAuthorized(config, AuthRequest{Internal: args.Value("internal") == true,
		Authorization: header, AuthorizationPresent: present, Token: token}, tokens, verify)
	return check(out, err)
}

// recordedResponse is the request_rule answer of a fetch_remote row.
type recordedBody struct {
	chunks [][]byte
	err    error
}

func (b *recordedBody) Read(p []byte) (int, error) {
	if len(b.chunks) == 0 {
		if b.err != nil {
			return 0, b.err
		}
		return 0, io.EOF
	}
	n := copy(p, b.chunks[0])
	b.chunks[0] = b.chunks[0][n:]
	if len(b.chunks[0]) == 0 {
		b.chunks = b.chunks[1:]
	}
	return n, nil
}

func (b *recordedBody) Close() error { return nil }

func replayFetchRemote(t *testing.T, label string, row *jsonx.Object, check func(any, error) bool) bool {
	args := row.GetList("args")
	note := row.GetObject("note")
	var config *jsonx.Object
	if args[2] == true {
		config = jsonx.Obj("system_config", args[1])
	}
	var gotRequest rulefetch.Request
	restore := SetEnv(&Env{RequestRule: func(_ context.Context, url string, r rulefetch.Request) (*rulefetch.Response, error) {
		gotRequest = r
		if spec := note.GetObject("err"); spec != nil {
			return nil, buildRecordedError(spec)
		}
		response := note.GetObject("response")
		header := http.Header{}
		if length, ok := response.Value("content_length").(string); ok {
			header.Set("Content-Length", length)
		}
		body := &recordedBody{}
		for _, chunk := range note.GetList("chunks") {
			data, _ := recordedBytes(chunk)
			body.chunks = append(body.chunks, data)
		}
		if spec := note.GetObject("iter_err"); spec != nil {
			body.err = buildRecordedError(spec)
		}
		return &rulefetch.Response{StatusCode: int(response.GetInt("status")), Header: header, URL: url, Body: body}, nil
	}})
	defer restore()
	out, err := FetchRemoteContent(context.Background(), args[0].(string), config, args[3] == true, "")
	request := note.GetObject("request")
	if request != nil && (gotRequest.ConnectTimeout.Seconds() != 3 || gotRequest.ReadTimeout.Seconds() != 10 ||
		!gotRequest.Stream || gotRequest.MaxRedirects == nil || *gotRequest.MaxRedirects != 3 ||
		(gotRequest.Config != nil) != (request.Value("config") == true)) {
		t.Errorf("%s: request options %+v, want %s", label, gotRequest, compact(request))
		return false
	}
	return check(out, err)
}

func replayBuild(t *testing.T, label string, row *jsonx.Object, check func(any, error) bool) bool {
	world, ok := seedWorld(t, row.GetObject("world"))
	if !ok {
		t.Logf("%s: world not reproducible", label)
		return true
	}
	args := row.GetList("args")
	limits := row.GetList("limits")
	savedFiles, savedTotal := MaxFiles, MaxTotalBytes
	MaxFiles, MaxTotalBytes = int(jsonx.AsInt(limits[0])), int(jsonx.AsInt(limits[1]))
	defer func() { MaxFiles, MaxTotalBytes = savedFiles, savedTotal }()
	answers := newFetchAnswers(row.GetObject("answers").GetList("fetch"))
	restore := SetEnv(&Env{FetchRemoteContent: answers.fetch})
	defer restore()
	config := args[0].(*jsonx.Object)
	downloads, _ := args[2].([]any)
	customFiles, _ := args[3].([]any)
	data, err := BuildMosDNSZip(world.ctx, config, args[1].(string), downloads, customFiles,
		ArchiveOptions{BaseURL: args[4].(string), SkipReferenceValidation: args[5] == false})
	var out any
	if err == nil {
		reader, zipErr := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if zipErr != nil {
			t.Fatalf("%s: %v", label, zipErr)
		}
		entries := []any{}
		for _, file := range reader.File {
			handle, _ := file.Open()
			content, _ := io.ReadAll(handle)
			handle.Close()
			entries = append(entries, []any{file.Name, content})
		}
		out = recordedEntries(entries)
	}
	if !check(out, err) {
		return false
	}
	if row.Value("err") == nil {
		if got, want := compact(ruleFiles(t, world.r)), compact(row.Value("after")); got != want {
			t.Errorf("%s: rule files after\n  got  %.400s\n  want %.400s", label, got, want)
			return false
		}
	}
	return true
}

func recordedEntries(entries []any) any {
	out := []any{}
	for _, entry := range entries {
		pair := entry.([]any)
		out = append(out, []any{pair[0], valueForCompare(pair[1])})
	}
	return out
}

func replayProxy(t *testing.T, label string, row *jsonx.Object, skipped map[string]int) bool {
	world, ok := seedWorld(t, row.GetObject("world"))
	if !ok {
		skipped["world"]++
		return true
	}
	request := row.GetObject("request")
	reqArgs := request.GetObject("args")
	answers := row.GetObject("answers")
	patched := row.GetObject("patched")
	argText := func(key string) string { s, _ := reqArgs.Value(key).(string); return s }
	header, present := request.Value("authorization").(string)
	proxyRequest := RuleProxyRequest{
		Auth: AuthRequest{Internal: request.Value("internal") == true, Authorization: header,
			AuthorizationPresent: present, Token: argText("token")},
		URL: argText("url"), Part: argText("part"), SourceID: argText("source_id"),
		HostURL: request.GetString("host_url"),
	}
	deps := defaultRuleProxyDeps()
	deps.getConfig = func(context.Context) (*jsonx.Object, error) {
		answer := firstAnswer(answers, "config")
		if answer == nil {
			return nil, errors.New("no recorded config")
		}
		if spec := answer.GetObject("err"); spec != nil {
			return nil, buildRecordedError(spec)
		}
		config, _ := answer.Value("ok").(*jsonx.Object)
		return config, nil
	}
	deps.authorized = func(context.Context, *jsonx.Object, AuthRequest) (bool, error) {
		answer := firstAnswer(answers, "auth")
		if answer == nil {
			return false, errors.New("no recorded auth")
		}
		if spec := answer.GetObject("err"); spec != nil {
			return false, buildRecordedError(spec)
		}
		return answer.Value("ok") == true, nil
	}
	if patched.Value("local") == true {
		deps.localContentRule = func(string, *jsonx.Object, string) (any, bool, error) {
			answer := firstAnswer(answers, "local")
			if spec := answer.GetObject("err"); spec != nil {
				return nil, false, buildRecordedError(spec)
			}
			value := answer.Value("ok")
			return value, value != nil, nil
		}
	}
	if patched.Value("github") == true {
		deps.applyGithubProxy = func(url string, _ *jsonx.Object) (string, error) {
			answer := firstAnswer(answers, "github")
			if spec := answer.GetObject("err"); spec != nil {
				return "", buildRecordedError(spec)
			}
			return answer.GetString("ok"), nil
		}
	}
	if patched.Value("validate") == true {
		deps.validateRemote = func(string) error {
			answer := firstAnswer(answers, "validate")
			if spec := answer.GetObject("err"); spec != nil {
				return buildRecordedError(spec)
			}
			return nil
		}
	}
	fetches := newFetchAnswers(answers.GetList("fetch"))
	restore := SetEnv(&Env{FetchRemoteContent: fetches.fetch})
	defer restore()
	response := ruleProxy(world.ctx, proxyRequest, deps)
	if wantErr := row.GetObject("err"); wantErr != nil {
		t.Errorf("%s: Python raised %s (views always answer)", label, compact(wantErr))
		return false
	}
	got := jsonx.Obj("status", jsonx.Int(int64(response.Status)))
	if response.JSON != nil {
		got.Set("json", response.JSON)
	} else {
		got.Set("text", response.Text)
	}
	got.Set("no_store", response.NoStore)
	got.Set("stale", response.Stale)
	if compact(got) != compact(row.Value("out")) {
		t.Errorf("%s:\n  got  %.600s\n  want %.600s", label, compact(got), compact(row.Value("out")))
		return false
	}
	if gotFiles, wantFiles := compact(ruleFiles(t, world.r)), compact(row.Value("after")); gotFiles != wantFiles {
		t.Errorf("%s: rule files after\n  got  %.400s\n  want %.400s", label, gotFiles, wantFiles)
		return false
	}
	return true
}
