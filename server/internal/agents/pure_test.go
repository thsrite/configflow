package agents

// Ports of test_operation_events.py, test_deployment_messages.py,
// test_agent_upgrade_messages.py (pure parts), test_agent_version_sync.py,
// test_idle_rounds.py and the pure cases of test_agent_failure_details.py.

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestEventHistoryIsBoundedAndAdjacentUnchangedProgressDoesNotDuplicate(t *testing.T) {
	var events any = []any{}
	start := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	for index := 0; index < 120; index++ {
		at := isoformat(start.Add(time.Duration(index)*time.Second), true)
		message := "规则文件 " + itoa(index) + "/120"
		events = AppendEvent(events, "preparing", message, at, nil)
		events = AppendEvent(events, "preparing", message, at, nil)
	}
	list := events.([]any)
	if len(list) != 80 || list[0].(*jsonx.Object).Value("message") != "规则文件 40/120" || list[79].(*jsonx.Object).Value("message") != "规则文件 119/120" {
		t.Fatalf("events %d", len(list))
	}
}

func itoa(n int) string { return jsonx.Int(n).String() }

func TestExternalEventsRejectInvalidShapesTimesAndStatusAndStripSecrets(t *testing.T) {
	events := CleanEvents([]any{
		nil, jsonx.NewObject(), jsonx.Obj("at", "not-time", "status", "checking"),
		jsonx.Obj("at", "0001-01-01T00:00:00+14:00", "status", "checking"),
		jsonx.Obj("at", "9999-12-31T23:59:59-14:00", "status", "checking"),
		jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "alien"),
		jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "checking",
			"message", "下载 https://user:password@example.test/secret-path?token=hidden AgentToken",
			"token", "extra-secret", "config", "private"),
	}, []any{"AgentToken"}, map[string]bool{"checking": true})
	if len(events) != 1 {
		t.Fatalf("events %v", events)
	}
	event := events[0].(*jsonx.Object)
	if strings.Join(event.Keys(), ",") != "at,status,message" || event.Value("at") != "2026-10-18T00:00:00.000000Z" {
		t.Fatalf("event %v", event)
	}
	text := jsonx.Repr(events)
	for _, secret := range []string{"password", "secret-path", "hidden", "AgentToken", "extra-secret", "private"} {
		if strings.Contains(text, secret) {
			t.Fatalf("%s leaked: %s", secret, text)
		}
	}
}

func TestRepeatedWorkerJournalMergesWithoutReplayingStages(t *testing.T) {
	events := AppendEvent([]any{}, "queued", nil, "2026-10-18T00:00:00Z", nil)
	incoming := []any{jsonx.Obj("at", "2026-10-18T00:00:01Z", "status", "queued"),
		jsonx.Obj("at", "2026-10-18T00:00:02Z", "status", "downloading", "message", "下载 Agent v1"),
		jsonx.Obj("at", "2026-10-18T00:00:03Z", "status", "verifying", "message", "检查候选版本")}
	merged := MergeEvents(events, incoming, nil, nil)
	var statuses []string
	for _, item := range merged {
		statuses = append(statuses, item.(*jsonx.Object).GetString("status"))
	}
	if strings.Join(statuses, ",") != "queued,downloading,verifying" || !jsonx.Equal(MergeEvents(merged, incoming, nil, nil), merged) {
		t.Fatalf("merged %v", merged)
	}
}

func TestEventTimesSortWholeSecondsBeforeFractionalSeconds(t *testing.T) {
	whole, _ := EventTime("2026-10-18T00:00:00Z")
	fraction, _ := EventTime("2026-10-18T00:00:00.1Z")
	next, _ := EventTime("2026-10-18T00:00:01Z")
	if !(whole < fraction && fraction < next) {
		t.Fatal(whole, fraction, next)
	}
	if _, ok := EventTime("2026-10-18T00:00:00"); ok {
		t.Fatal("naive accepted")
	}
}

func TestPublicHistoryLocalizesErrorsWithoutMutatingSavedEvents(t *testing.T) {
	raw := jsonx.Obj("events", []any{jsonx.Obj("at", "2026-10-18T00:00:00Z", "status", "failed", "message", "Agent download size or SHA-256 mismatch")})
	public := PublicUpgradeState(raw)
	if !strings.Contains(public.GetList("events")[0].(*jsonx.Object).GetString("message"), "校验失败") ||
		raw.GetList("events")[0].(*jsonx.Object).Value("message") != "Agent download size or SHA-256 mismatch" {
		t.Fatal("upgrade")
	}
	deployment := PublicDeploymentMessages(jsonx.Obj("events", []any{jsonx.Obj("at", EventTimeNow(), "status", "failed", "message", "service start failed")}))
	if !strings.Contains(deployment.GetList("events")[0].(*jsonx.Object).GetString("message"), "服务启动失败") {
		t.Fatal("deployment")
	}
}

func TestKnownFailuresKeepTheExactOriginal(t *testing.T) {
	for _, c := range [][2]string{
		{"Deployment preparation failed", "准备发布文件失败"},
		{"Upload outcome unknown; query deployment status", "暂时无法确认上传结果"},
		{"Rollback outcome unknown; awaiting Agent status", "暂时无法确认回滚结果"},
		{"Only a successful deployment can be rolled back", "发布成功"},
		{"service start failed", "服务启动失败"},
		{"service is not running: ERROR listen udp 0.0.0.0:53: bind: address already in use", "核心服务启动后退出"},
		{"timezone data for Asia/Shanghai is missing on this device; install tzdata (Alpine: apk add tzdata) and publish again", "apk add tzdata"},
		{"backup checksum mismatch: rules/Claude.mrs", "旧配置备份校验失败"},
		{"rollback cannot stop service: rc-service exited with code 1", "回滚时无法停止当前服务"},
		{"Agent returned HTTP 503", "HTTP 状态码"},
		{"Rule Claude: Invalid rule YAML payload", "规则集准备失败"},
	} {
		display := DeploymentMessage(c[0]).(string)
		parts := strings.SplitN(display, "\n原始错误：", 2)
		if !strings.Contains(display, c[1]) || len(parts) != 2 || parts[1] != c[0] || DeploymentMessage(display) != display {
			t.Fatalf("%q -> %q", c[0], display)
		}
	}
}

func TestUnknownCoreAndSourceErrorsAreNotTranslated(t *testing.T) {
	for _, raw := range []string{
		"yaml: unmarshal errors:\n  line 6: cannot unmarshal !!str into config.RawConfig",
		"upstream https://dns.example/dns-query returned status 503: SERVFAIL",
		"invalid value for argument new-routing-mark",
		"Query Refused (REFUSED)",
		"配置已保存，等待发布",
	} {
		if DeploymentMessage(raw) != raw {
			t.Fatalf("%q translated", raw)
		}
	}
}

func TestUpgradeFailuresHaveActionableChineseMessages(t *testing.T) {
	for _, c := range [][2]string{
		{"Agent download connection failed", "无法连接 Agent 更新下载地址"},
		{"Agent download HTTP 404", "服务器返回 HTTP 404"},
		{"Agent download write failed", "下载或写入失败"},
		{"Agent download size or SHA-256 mismatch", "文件大小或 SHA-256 与预期不符"},
		{"candidate version or executable validation failed", "新版 Agent 无法运行或版本不符"},
		{"Agent did not confirm expected version, identity and migration readiness", "未能确认 Agent 的版本"},
		{"another Agent update or service operation is active", "请等待完成后再试"},
		{"an OxiDNS rule update is in progress", "OxiDNS 正在更新规则"},
		{"deployment previous-job requires rollback recovery", "配置发布尚未完成恢复"},
		{"OpenRC supervise-daemon is required for recoverable updates", "设备缺少 OpenRC"},
		{"cannot infer custom lifecycle; configure service_manager and service_unit before upgrading", "请先配置 service_manager 和 service_unit"},
		{"open /opt/agent: permission denied", "权限不足"},
		{"write /opt/agent: no space left on device", "磁盘空间不足"},
		{"rename /opt/agent: read-only file system", "文件系统只读"},
		{"open /opt/agent: no such file or directory", "文件或目录不存在"},
		{"dial tcp: lookup config.test: no such host", "无法解析服务器地址"},
		{"context deadline exceeded", "操作超时"},
		{"systemctl operation failed: Job failed with exit status 1", "服务管理命令执行失败"},
		{"rc-service operation failed: permission denied", "权限不足"},
		{"new unexpected worker failure", "Agent 更新遇到异常"},
		{"   ", "Agent 更新遇到异常"},
	} {
		message := UpgradeErrorMessage(c[0])
		if !strings.Contains(message, c[1]) || (strings.TrimSpace(c[0]) != "" && !strings.HasSuffix(message, "原始错误："+c[0])) ||
			UpgradeErrorMessage(message) != message {
			t.Fatalf("%q -> %q", c[0], message)
		}
	}
}

func TestFailedUpdateAndFailedRollbackBothRemainVisible(t *testing.T) {
	raw := "candidate version or executable validation failed; rollback: Agent backup hash mismatch"
	message := UpgradeErrorMessage(raw)
	for _, want := range []string{"新版 Agent 无法运行或版本不符", "恢复旧版本失败", "备份文件校验失败"} {
		if !strings.Contains(message, want) {
			t.Fatalf("%q missing %q", message, want)
		}
	}
	if strings.Contains(message, "已回滚") || !strings.HasSuffix(message, "原始错误："+raw) || strings.Count(message, "原始错误：") != 1 {
		t.Fatal(message)
	}
	nested := UpgradeErrorMessage("migration failed; configuration rollback failed: open /opt/agent/config.json: permission denied")
	if !strings.Contains(nested, "配置迁移失败，恢复原配置也失败") || !strings.Contains(nested, "权限不足") {
		t.Fatal(nested)
	}
}

func TestResponseCopyPreservesProtocolFieldsAndOriginalError(t *testing.T) {
	state := jsonx.Obj("update_id", "job", "status", "rollback_failed", "protocol", 2, "target_version", "1.8.16-go",
		"previous_version", "1.8.15-go", "error", "Agent backup hash mismatch", "updated_at", 123,
		"artifacts", jsonx.Obj("linux-amd64", jsonx.Obj("size", 100, "sha256", strings.Repeat("a", 64))))
	original := state.DeepCopy()
	public := PublicUpgradeState(state)
	if !jsonx.Equal(state, original) || public.Value("error") == state.Value("error") || !strings.HasSuffix(public.GetString("error"), "原始错误：Agent backup hash mismatch") {
		t.Fatal(public)
	}
	a, b := public.Clone(), original.Clone()
	a.Delete("error")
	b.Delete("error")
	if !jsonx.Equal(a, b) || !jsonx.Equal(PublicUpgradeState(public), public) {
		t.Fatal("protocol fields changed")
	}
	for _, kept := range []*jsonx.Object{jsonx.NewObject(), jsonx.Obj("status", "succeeded", "error", ""),
		jsonx.Obj("status", "checking", "error", "请求结果待确认，正在查询 Agent")} {
		if !jsonx.Equal(PublicUpgradeState(kept), kept) {
			t.Fatal(kept)
		}
	}
}

func TestGoAgentVersionMatchesLatestAgentVersion(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "backend", "agents", "go-agent", "upgrade", "migration.go"))
	if err != nil {
		t.Fatal(err)
	}
	versions := regexp.MustCompile(`(?m)^const Version = "([^"]+)"$`).FindAllStringSubmatch(string(data), -1)
	if len(versions) != 1 || versions[0][1] != LatestAgentVersion {
		t.Fatalf("versions %v", versions)
	}
}

func TestLatestAgentVersionHasChangelogEntry(t *testing.T) {
	entry, ok := VersionChangelog(LatestAgentVersion)
	if !ok || !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(entry.Date) || len(entry.Features) == 0 || Changelog()[0].Version != LatestAgentVersion {
		t.Fatal(entry)
	}
	for _, feature := range entry.Features {
		if feature == "" {
			t.Fatal("empty feature")
		}
	}
	if ChangelogObject().Len() != len(Changelog()) {
		t.Fatal("duplicate versions")
	}
}

func TestExistingAgentsCanUpdate(t *testing.T) {
	for _, version := range []string{"1.8.47-go", "1.8.48-go", "1.8.49-go", "1.8.50-go", "1.8.51-go"} {
		if !HasUpdate(version) {
			t.Fatal(version)
		}
	}
	if HasUpdate(LatestAgentVersion) {
		t.Fatal("latest")
	}
}

func TestCompareVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want int
	}{
		{"1.0", "1.0.0", 0}, {"1.10.0", "1.9.9", 1}, {"1.1.0-go", "1.1.0", 0}, {"1.a", "1.b", -1},
		{"1.8.61-go", "1.8.7-go", 1}, {" 2", "1", 1}, {"1_0", "9", 1}, {"", "0", -1},
	} {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Fatalf("%q vs %q = %d", c.a, c.b, got)
		}
	}
}

// test_agent_failure_details.py (pure parts)

func TestRedactionKeepsRuleNamesAndFileLocations(t *testing.T) {
	text := FailureText("\x1b[31mconfig.yaml:42: missing rule MyProxy\x1b[0m\n"+
		"fetch https://name:pass@rules.test/list?token=hidden&format=yaml\n"+
		"{\"password\": \"a secret\", \"api_key\": \"hide-this\"}\nAuthorization: Bearer xyz", nil, 0)
	if !strings.Contains(text, "config.yaml:42: missing rule MyProxy") || !strings.Contains(text, "rules.test/list") || !strings.Contains(text, "format=yaml") {
		t.Fatal(text)
	}
	for _, secret := range []string{"name:pass", "hidden", "a secret", "hide-this", "xyz", "\x1b"} {
		if strings.Contains(text, secret) {
			t.Fatalf("%q in %q", secret, text)
		}
	}
}

func TestMultilineAndEscapedCredentialsAreHidden(t *testing.T) {
	for _, credential := range []string{
		"password: |\n  private-first\n  private-second\n",
		"private-key: >-\n  private-first\n\n  private-second\n",
		"  - password: |\n      private-first\n      private-second\n",
		`password: "first \"private-first\" private-second"` + "\n",
		"password: 'first ''private-first'' private-second'\n",
		"password: \"private-first\n  private-second\"\n",
		"-----BEGIN PRIVATE KEY-----\nprivate-first\nprivate-second\n-----END PRIVATE KEY-----\n",
	} {
		detail := FailureText("config.yaml:42: invalid credential\n"+credential+"next failure: upstream unavailable", nil, 0)
		if strings.Contains(detail, "private-first") || strings.Contains(detail, "private-second") ||
			!strings.Contains(detail, "config.yaml:42") || !strings.Contains(detail, "next failure: upstream unavailable") {
			t.Fatalf("%q -> %q", credential, detail)
		}
	}
	detail := FailureText("  password: |\n    private-first\n  error: bad proxy name\n", nil, 0)
	if strings.Contains(detail, "private-first") || !strings.Contains(detail, "error: bad proxy name") {
		t.Fatal(detail)
	}
}

// test_idle_rounds.py

func commitProfile(t *testing.T, e *env, value string) {
	if _, _, err := e.repo.UpdateProfileFields(e.ctx, "default", jsonx.Obj("description", value), nil); err != nil {
		t.Fatal(err)
	}
}

func idleEnv(t *testing.T) *env {
	dir := t.TempDir()
	return &env{t: t, ctx: context.Background(), repo: openRepo(t, dir), dir: dir}
}

func due(t *testing.T, rounds *IdleRounds, e *env) (any, bool) {
	head, run, err := rounds.Due(e.ctx, e.repo)
	if err != nil {
		t.Fatal(err)
	}
	return head, run
}

func TestRoundRunsUntilOneReportsIdleAndAgainAfterEveryCommit(t *testing.T) {
	e := idleEnv(t)
	rounds := NewIdleRounds(nil)
	headAny, run := due(t, rounds, e)
	current, _ := e.repo.Head(e.ctx)
	if !run || headAny != any(current) {
		t.Fatal("first round")
	}
	head := current
	if _, run := due(t, rounds, e); !run {
		t.Fatal("unreported round skipped")
	}
	rounds.Done(e.repo, head, false)
	if _, run := due(t, rounds, e); !run {
		t.Fatal("busy round skipped")
	}
	rounds.Done(e.repo, head, true)
	if _, run := due(t, rounds, e); run {
		t.Fatal("idle round ran")
	}
	commitProfile(t, e, "x")
	afterAny, run := due(t, rounds, e)
	after, _ := e.repo.Head(e.ctx)
	if !run || afterAny != any(after) || after == head {
		t.Fatal("commit")
	}
	rounds.Done(e.repo, after, true)
	if _, run := due(t, rounds, e); run {
		t.Fatal("idle again")
	}
	rounds.Done(e.repo, after, false)
	if _, run := due(t, rounds, e); !run {
		t.Fatal("busy forgets idle")
	}
}

func TestCommitDuringTheRoundMakesTheNextRoundRun(t *testing.T) {
	e := idleEnv(t)
	rounds := NewIdleRounds(nil)
	head, _, _ := rounds.Due(e.ctx, e.repo)
	commitProfile(t, e, "x")
	rounds.Done(e.repo, head, true)
	if _, run := due(t, rounds, e); !run {
		t.Fatal("skipped")
	}
}

func TestWriteByAnotherProcessIsNoticed(t *testing.T) {
	e := idleEnv(t)
	rounds := NewIdleRounds(nil)
	head, _, _ := rounds.Due(e.ctx, e.repo)
	rounds.Done(e.repo, head, true)
	if _, run := due(t, rounds, e); run {
		t.Fatal("idle")
	}
	other := &env{t: t, ctx: e.ctx, repo: openRepo(t, e.dir)}
	commitProfile(t, other, "elsewhere")
	if _, run := due(t, rounds, e); !run {
		t.Fatal("external commit not noticed")
	}
}

func TestIdleRoundIsRepeatedAfterTheRecheckIntervalAndAfterAClockStepBack(t *testing.T) {
	e := idleEnv(t)
	now := 1000.0
	rounds := NewIdleRounds(func() float64 { return now })
	head, _, _ := rounds.Due(e.ctx, e.repo)
	rounds.Done(e.repo, head, true)
	now += RecheckSeconds - 1
	if _, run := due(t, rounds, e); run {
		t.Fatal("early")
	}
	now++
	if _, run := due(t, rounds, e); !run {
		t.Fatal("recheck")
	}
	rounds.Done(e.repo, head, true)
	if _, run := due(t, rounds, e); run {
		t.Fatal("idle")
	}
	now -= 5000
	if _, run := due(t, rounds, e); !run {
		t.Fatal("clock step back")
	}
}

func TestRepositoriesAndLoopsDoNotShareIdleState(t *testing.T) {
	e := idleEnv(t)
	first, second := NewIdleRounds(nil), NewIdleRounds(nil)
	head, _, _ := first.Due(e.ctx, e.repo)
	first.Done(e.repo, head, true)
	if _, run := due(t, first, e); run {
		t.Fatal("idle")
	}
	if _, run := due(t, second, e); !run {
		t.Fatal("shared between loops")
	}
	idleRoundOfAnotherRepository(t, first)
	// Nothing keeps a discarded repository (and its cached document) alive.
	for i := 0; i < 100 && first.Len() != 1; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if first.Len() != 1 {
		t.Fatalf("discarded repository still tracked: len %d", first.Len())
	}
	// The fixture's repository is still in use (Python's fixture keeps it).
	runtime.KeepAlive(e.repo)
}

// idleRoundOfAnotherRepository records an idle round of a repository that is
// unreachable once it returns (not inlined, so no stack slot keeps it).
//
//go:noinline
func idleRoundOfAnotherRepository(t *testing.T, rounds *IdleRounds) {
	other := idleEnv(t)
	head, run, err := rounds.Due(other.ctx, other.repo)
	if err != nil || !run {
		t.Fatal("shared between repositories")
	}
	rounds.Done(other.repo, head, true)
	if rounds.Len() != 2 {
		t.Fatal("len")
	}
	runtime.KeepAlive(other.repo)
}
