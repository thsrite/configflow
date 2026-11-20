package mosdns

// Archive acceptance: the build_mosdns_zip calls the whole Python suite made
// (testdata/c28_recorded.jsonl.gz, rows "build", from c28_record.py) are
// rebuilt in Go from the same snapshot, rule caches and download answers;
// the archive entries (names, order, bytes) and the rule caches written
// afterwards must equal Python's, failures must be refused with the same
// Chinese message. Hand-written tests cover concurrency and budgets.

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/taskstatus"
)

func hexBytes(v any) ([]byte, bool) {
	if o, ok := v.(*jsonx.Object); ok {
		if text, ok := o.Value("__bytes__").(string); ok {
			data, _ := hex.DecodeString(text)
			return data, true
		}
	}
	return nil, false
}

// testRepo opens a fresh repository with the recorded shared rule library,
// rule cache files and task-status.json.
func testRepo(t *testing.T, world *jsonx.Object) (context.Context, *repo.Repository) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithScope(repo.WithRepository(context.Background(), r))
	if library := world.GetList("rule_library"); len(library) > 0 {
		if _, err := r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
			shared.Set("rule_library", jsonx.DeepCopy(library))
			return nil, nil
		}); err != nil {
			t.Fatalf("seed library: %v", err)
		}
	}
	rulesDir, _ := r.SharedRulesDir()
	for name, value := range world.GetObject("files").All() {
		data, _ := hexBytes(value)
		path := filepath.Join(rulesDir, strings.TrimPrefix(name, "rules/"))
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, data, 0o644)
	}
	if text, ok := world.Value("task_status").(string); ok {
		os.WriteFile(filepath.Join(r.DataDir(), taskstatus.Filename), []byte(text), 0o644)
	}
	return ctx, r
}

func ruleCacheFiles(r *repo.Repository) map[string]string {
	rulesDir, _ := r.SharedRulesDir()
	out := map[string]string{}
	filepath.WalkDir(rulesDir, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() && !strings.Contains(entry.Name(), ".tmp") && !strings.HasSuffix(entry.Name(), ".lock") {
			rel, _ := filepath.Rel(rulesDir, path)
			data, _ := os.ReadFile(path)
			out["rules/"+filepath.ToSlash(rel)] = string(data)
		}
		return nil
	})
	return out
}

func readZip(t *testing.T, data []byte) [][2]string {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out [][2]string
	for _, file := range reader.File {
		handle, _ := file.Open()
		content, _ := io.ReadAll(handle)
		handle.Close()
		out = append(out, [2]string{file.Name, string(content)})
	}
	return out
}

// answers replays the recorded _fetch_remote_content results per URL.
type answers struct {
	mu    sync.Mutex
	byURL map[string][]*jsonx.Object
}

func (a *answers) fetch(_ context.Context, url string, _ *model.Config, _ string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	queue := a.byURL[url]
	if len(queue) == 0 {
		// Python failed before it reached this download: answer late so
		// the recorded downloads decide the outcome.
		a.mu.Unlock()
		time.Sleep(2 * time.Second)
		a.mu.Lock()
		return nil, &rulefetch.FetchError{Class: "ConnectionError", Message: "unrecorded " + url}
	}
	answer := queue[0]
	if len(queue) > 1 {
		a.byURL[url] = queue[1:]
	}
	if spec := answer.GetObject("err"); spec != nil {
		class, message := spec.GetString("class"), spec.GetString("message")
		switch {
		case spec.Value("requests") == true:
			return nil, &rulefetch.FetchError{Class: class, Message: message, Status: int(spec.GetInt("status"))}
		case class == "RemoteRuleValidationError":
			return nil, rulefetch.NewRemoteRuleMessage(message)
		}
		return nil, errors.New(message)
	}
	if data, ok := hexBytes(answer.Value("ok")); ok {
		return data, nil
	}
	return []byte(answer.GetString("ok")), nil
}

func withFetch(t *testing.T, fn nativeFetchFunc) {
	saved := fetchNativeFn
	fetchNativeFn = fn
	t.Cleanup(func() { fetchNativeFn = saved })
}

func decodeDownloads(list []any) ([]Download, error) {
	data, err := jsonx.Compact(list)
	if err != nil {
		return nil, err
	}
	var out []Download
	return out, jsondecode.Unmarshal(data, &out)
}

func TestArchiveMatchesRecordedPythonBuilds(t *testing.T) {
	rows := loadRows(t, "testdata/c28_recorded.jsonl.gz")
	references := archiveCacheReferences(t, rows)
	usedReferences := 0
	built, failures := 0, 0
	for i, row := range rows {
		// The cancellation test races its downloads (only the first ones are
		// recorded); TestArchiveFailureDoesNotWaitForOtherDownloads covers it.
		if row.GetString("fn") != "build" || row.Has("unsupported") ||
			strings.Contains(row.GetString("test"), "cancels_queue_without_waiting") {
			continue
		}
		world := row.GetObject("world")
		if id := world.GetString("default_profile"); id != "default" {
			continue
		}
		name := row.GetString("test") + " #" + jsonx.Str(i)
		reference, approved := references[i]
		if approved {
			usedReferences++
		}
		args := row.GetList("args")
		ctx, r := testRepo(t, world)
		limits := row.GetList("limits")
		savedFiles, savedTotal := MaxFiles, MaxTotalBytes
		files, _ := jsonx.AsInt(limits[0])
		total, _ := jsonx.AsInt(limits[1])
		MaxFiles, MaxTotalBytes = int(files), int(total)
		recorded := &answers{byURL: map[string][]*jsonx.Object{}}
		for _, item := range row.GetObject("answers").GetList("fetch") {
			answer := item.(*jsonx.Object)
			url := answer.GetList("args")[0].(string)
			recorded.byURL[url] = append(recorded.byURL[url], answer)
		}
		saved := fetchNativeFn
		fetchNativeFn = recorded.fetch
		downloads, err := decodeDownloads(args[2].([]any))
		if err != nil {
			t.Fatalf("%s: downloads %v", name, err)
		}
		var custom []File
		for _, item := range args[3].([]any) {
			object := item.(*jsonx.Object)
			content, ok := hexBytes(object.Value("content"))
			if !ok {
				content = []byte(object.GetString("content"))
			}
			custom = append(custom, File{Path: object.GetString("path"), Content: string(content)})
		}
		nativeCfg, err := legacyConfig(args[0].(*jsonx.Object))
		if err != nil {
			t.Fatalf("%s: snapshot %v", name, err)
		}
		data, err := BuildZIP(ctx, nativeCfg, args[1].(string), downloads, custom,
			ArchiveOptions{BaseURL: args[4].(string), SkipReferenceValidation: args[5] == false})
		fetchNativeFn = saved
		MaxFiles, MaxTotalBytes = savedFiles, savedTotal
		built++
		if reference.Kind == "unowned_refusal" {
			var refusal *ArchiveError
			if !errors.As(err, &refusal) || err.Error() != reference.Error || len(data) != 0 {
				t.Errorf("%s: exact unowned source refusal: %v", name, err)
			}
			if !equalMaps(ruleCacheFiles(r), archiveExpectedCaches(t, row, reference)) {
				t.Errorf("%s: unowned source changed cache files", name)
			}
			continue
		}
		if want := row.GetObject("err"); want != nil {
			var archive *ArchiveError
			if !errors.As(err, &archive) || err.Error() != want.GetString("message") {
				failures++
				t.Errorf("%s:\n  got  %v\n  want %s %q", name, err, want.GetString("class"), want.GetString("message"))
			}
			continue
		}
		if err != nil {
			failures++
			t.Errorf("%s: %v", name, err)
			continue
		}
		var want [][2]string
		for _, item := range row.GetList("out") {
			pair := item.([]any)
			content, ok := hexBytes(pair[1])
			if !ok {
				content = []byte(pair[1].(string))
			}
			want = append(want, [2]string{pair[0].(string), string(content)})
		}
		if got := readZip(t, data); !equalEntries(got, want) {
			failures++
			t.Errorf("%s: entries\n  got  %.500q\n  want %.500q", name, got, want)
			continue
		}
		after := archiveExpectedCaches(t, row, reference)
		if got := ruleCacheFiles(r); !equalMaps(got, after) {
			failures++
			t.Errorf("%s: rule caches after\n  got  %v\n  want %v", name, keys(got), keys(after))
		}
	}
	if usedReferences != len(references) {
		t.Fatal("archive D2 references were not all exercised")
	}
	if built == 0 {
		t.Fatal("no recorded builds")
	}
	t.Logf("%d recorded builds, %d differ", built, failures)
}

func equalEntries(a, b [][2]string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// remoteSnapshot is a profile whose downloads all go to remote sources.
func remoteSnapshot() *model.Config {
	return &model.Config{
		ProfileID: "office",
		SystemConfig: model.SystemConfig{
			ServerDomain:   "https://cf.test/prefix",
			RuleProxyToken: "internal-secret",
		},
		Profile:     &model.Profile{RuleConfigs: []model.RuleConfig{}},
		RuleLibrary: []model.RuleLibraryItem{},
	}
}

func remoteDownloads(n int) []Download {
	out := make([]Download, n)
	for i := range out {
		url := "https://rules.test/" + jsonx.Str(i)
		out[i] = Download{Name: "测试规则", LocalPath: "rules/" + jsonx.Str(i) + ".txt",
			URL: "https://cf.test/prefix/api/profiles/office/mosdns/rule-proxy?token=internal-secret&url=" + url}
	}
	return out
}

func TestArchiveFetchesAreParallelAndBounded(t *testing.T) {
	ctx, _ := testRepo(t, jsonx.NewObject())
	var active, peak, count int32
	withFetch(t, func(context.Context, string, *model.Config, string) ([]byte, error) {
		now := atomic.AddInt32(&active, 1)
		atomic.AddInt32(&count, 1)
		for {
			old := atomic.LoadInt32(&peak)
			if now <= old || atomic.CompareAndSwapInt32(&peak, old, now) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return []byte("domain:parallel.test"), nil
	})
	data, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", remoteDownloads(19), nil, ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&peak) != MaxWorkers || atomic.LoadInt32(&count) != 19 || len(readZip(t, data)) != 20 {
		t.Fatalf("peak %d count %d", peak, count)
	}
}

func TestArchiveFailureDoesNotWaitForOtherDownloads(t *testing.T) {
	ctx, _ := testRepo(t, jsonx.NewObject())
	release := make(chan struct{})
	defer close(release)
	var calls int32
	withFetch(t, func(_ context.Context, url string, _ *model.Config, _ string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		if strings.HasSuffix(url, "/0") {
			return nil, &rulefetch.FetchError{Class: "ReadTimeout", Message: "source-secret"}
		}
		<-release
		return []byte("domain:x.test"), nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", remoteDownloads(80), nil, ArchiveOptions{})
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || err.Error() != "规则“测试规则”下载失败：exception_type=ReadTimeout" {
			t.Fatalf("err %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waited for the other downloads")
	}
	if n := atomic.LoadInt32(&calls); n > MaxWorkers {
		t.Fatalf("%d downloads started", n)
	}
}

func TestArchiveBudgets(t *testing.T) {
	ctx, _ := testRepo(t, jsonx.NewObject())
	var calls int32
	withFetch(t, func(context.Context, string, *model.Config, string) ([]byte, error) {
		atomic.AddInt32(&calls, 1)
		return []byte("domain:" + strings.Repeat("long-label.", 8) + "test"), nil
	})
	savedFiles, savedTotal := MaxFiles, MaxTotalBytes
	defer func() { MaxFiles, MaxTotalBytes = savedFiles, savedTotal }()
	MaxTotalBytes = 80
	_, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", remoteDownloads(80), nil, ArchiveOptions{})
	if err == nil || !strings.Contains(err.Error(), "大小限制") || atomic.LoadInt32(&calls) > 2*MaxWorkers {
		t.Fatalf("size: %v after %d downloads", err, atomic.LoadInt32(&calls))
	}
	MaxTotalBytes, MaxFiles = savedTotal, 2
	time.Sleep(200 * time.Millisecond) // the first build's last downloads finish
	var late int32
	withFetch(t, func(context.Context, string, *model.Config, string) ([]byte, error) {
		atomic.AddInt32(&late, 1)
		return nil, errors.New("must not fetch")
	})
	_, err = BuildZIP(ctx, remoteSnapshot(), "plugins: []", remoteDownloads(2), nil, ArchiveOptions{})
	if err == nil || err.Error() != "生成配置超过打包文件数量限制" || atomic.LoadInt32(&late) != 0 {
		t.Fatalf("files: %v", err)
	}
}

func TestArchivePathsAndReferences(t *testing.T) {
	ctx, _ := testRepo(t, jsonx.NewObject())
	for _, path := range []string{"../escape", "/absolute", "./rules/../../escape", "rules\\escape", "config.yaml", "a//b", "\x01"} {
		if _, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", nil, []File{{path, "x"}}, ArchiveOptions{}); err == nil {
			t.Errorf("%q accepted", path)
		}
	}
	_, err := BuildZIP(ctx, remoteSnapshot(), "plugins: []", nil, []File{
		{"./rules/same.txt", "domain:a.test"}, {"rules/same.txt", "domain:b.test"}}, ArchiveOptions{})
	if err == nil || err.Error() != "生成配置包含内容冲突的文件路径" {
		t.Errorf("conflict: %v", err)
	}
	config := "plugins:\n- type: domain_set\n  args:\n    files: [./rules/missing.txt]"
	if _, err := BuildZIP(ctx, remoteSnapshot(), config, nil, nil, ArchiveOptions{}); err == nil ||
		err.Error() != "MosDNS 配置引用了未打包的规则文件" {
		t.Errorf("missing reference: %v", err)
	}
	if _, err := BuildZIP(ctx, remoteSnapshot(), config, nil, nil, ArchiveOptions{SkipReferenceValidation: true}); err != nil {
		t.Errorf("skipped validation: %v", err)
	}
	if got := label("https://x.test/?token=1"); got != "未命名规则" {
		t.Errorf("label %q", got)
	}
}

func TestArchiveEarlyFailureWorkersRemainTracked(t *testing.T) {
	base, _ := testRepo(t, jsonx.NewObject())
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
	runtime.Start(base)
	defer runtime.Shutdown(context.Background())
	release, all, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var started, stopped atomic.Int32
	withFetch(t, func(ctx context.Context, url string, _ *model.Config, _ string) ([]byte, error) {
		if started.Add(1) == MaxWorkers {
			close(all)
		}
		<-all
		if strings.HasSuffix(url, "/0") {
			return nil, &rulefetch.FetchError{Class: "ReadTimeout", Message: "synthetic"}
		}
		<-ctx.Done()
		if stopped.Add(1) == MaxWorkers-1 {
			close(cancelled)
		}
		<-release
		return nil, ctx.Err()
	})
	if _, err := BuildZIP(runtime.BindRequest(base), remoteSnapshot(), "plugins: []", remoteDownloads(MaxWorkers), nil, ArchiveOptions{}); err == nil {
		t.Fatal("failure hidden")
	}
	waiting, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtime.WaitIdle(waiting); !errors.Is(err, context.Canceled) {
		t.Fatal("early-return workers were not tracked", err)
	}
	done := make(chan struct{})
	go func() { runtime.Shutdown(t.Context()); close(done) }()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("archive I/O did not receive Runtime cancellation")
	}
	close(release)
	<-done
	if err := runtime.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
}
