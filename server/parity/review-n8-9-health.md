# N8-9 health/check 独立复核

复核者 Codex（非作者）；分支 `go-review-n8-9-health`，审查对象
`go-n8-9-health@d6cd8df63`，包括原样 Gemini 草稿 `33fbaabe0`。
依赖基线保持作者的 `batch-6@824189f07`，不混入同批 discovery 分支。
本次修复提交：`0f301dc67`。

结论：typed 迁移和作者的 11 项草稿更正通过本次范围的独立复核。
新增发现 **0 high、1 medium、1 low，均在 health 范围修复**；另有一个明确的
**medium 跨模块后续项**，协调者裁定 batch-7 合入后另派，未声称全链路关机窗口已关闭。
只在 `.32` 通过 rt 跑定向检查；不重跑作者整套，不跑完整门禁，不 push、不起子 agent。

## Medium — M1：删除 hook 丢失 owner ctx，关机后仍 unlink

- 场景：`Runtime.Start` → `BindRequest` → `Shutdown` 后调用 health 的
  `deleteProbeStore` / `deleteRegionStore`，两个函数取得 repository 后直接调用
  不接收 ctx 的 `store.Delete`，文件仍被删除。此为迁移触及入口审查中发现的既有窗口。
- 修复前证据：初始 `TestReviewN89StoppedDeleteHooksKeepFiles` 两个子例均失败，
  `stopped hook returned <nil>`，原合成文件 `no such file or directory`。
- 修复：hook 把 `lifecycle.Detach(ctx)` 传给各自 `DeleteContext`；store 在入口和
  获得真实锁后检查取消。本次同时为 `ProbeStore.SaveEntries` / `RegionStore.UpdateData`
  增加 `SaveEntriesContext` / `UpdateDataContext`，入口、锁后和提交前检查 ctx。
  原无 ctx API 仅作兼容包装，原存储校验、排序、剪枝、原子写协议不变。
- 回归：`TestReviewN89DeleteHooksRespectOwnerLifetime` 的两种 hook × 关机/断开；
  `TestReviewN89StoreCommitLifetime` 的四种入口 × 关机/断开，在第一次 Err 捕获 nil
  后设置屏障，模拟提交前被抢占，取消来自真实 Runtime generation；
  `TestReviewN89RegionUpdateChecksLifetimeAfterMutator` 覆盖 mutator 之后的最终检查。
  关机保留原文件字节，断开请求后实际写/删成功。未用单独 cancel context 冒充 Shutdown。
- 修复提交：`0f301dc67`。共享叶层中的剩余窗口见后续项 F1，不在本条闭合声明内。

## Low — L1：worker 已取消，外层却改报组件无法启动

- 场景：真正的子进程已运行，Runtime 关机使 `ExecRunner.Run` 返回
  `context.Canceled`；`runProbeFields` / `CheckDNSSettingsJSON` 的一般错误分支
  将其改成 `ProbeUnavailable`，上层不能再识别生命周期取消。
- 修复前证据：实际 `RunLatencyTests` → `ExecRunner` → shell worker 的回归失败：
  `shutdown error 测速组件无法启动，请检查可执行权限和 CPU 架构`。
  作者已有测试只直接测 ExecRunner，没覆盖这个包装层。
- 修复：两条 worker 路径调用 Runner 后首先检查原 owner ctx；保留
  Canceled/DeadlineExceeded 错误链。worker 自己预算耗尽仍按原 ErrTimeout 业务返回，
  启动失败、退出码、协议版本、stderr 脱敏处理不变。
- 回归：`TestReviewN89RealWorkerLifetimeControlsPersistence` 使用默认 ExecRunner、
  原生产预算、实际 shell 子进程和就绪屏障，latency/DNS × 关机/断开。
  关机返回 Canceled，latency 主文件和 `.bak` 均未产生；断开后 latency 结果和单次
  history 真正落盘，DNS 真正解析合成 stdout 成功。没有缩短生产 timeout 或换 fake runner。
  `TestReviewN89DNSWriteRespectsOwnerLifetime` 另检查 DNS 写入口：关机整个 System 不变，
  断开后结构化配置与镜像正常提交。C16 没有 taskstatus owner，未虚构任务状态检查。
- 修复提交：`0f301dc67`。

## Medium 后续 F1：共享 jsonstore 的最终提交窗口及 discovery 接线（本次不改）

协调者明确裁定：本次只修 health 自己层的检查；共享叶层 Context API 和 discovery
两处 adapter 在 batch-7 合入后另派，避免与 `go-n8-9-discovery` 冲突。
这仍是实际待办，不能因本次范围通过就写成整个关机协议 verified。

- `server/internal/jsonstore/jsonstore.go`：`Store.Write` 在编码、打开/写临时文件之后
  直接 `os.Rename`（普通和签名模式各一处）；`Store.Remove` 直接 `syscall.Unlink`，
  都没有 ctx。health 的提交前检查之后，若在这些 I/O 内关机，仍存在最终提交窗口。
- 需要共享 `WriteContext(ctx, id, data, verifySignature)` /
  `RemoveContext(ctx, id)`，保留兼容 wrapper；入口和主 rename / unlink 之前检查 ctx。
  rename 成功后的签名验证应完成，不应把已提交写入改报成取消；失败前临时文件应清理。
  `health/check/{probe,region}.go` 的三个 Context 入口改为调用这些叶层 API。
- discovery 当前基线 `bridge.go` 的 `saveProbes` / `editRegion`，同批迁移分支
  `legacy.go` 的 `legacySaveProbes` / `legacyEditRegion`，最终仍调用无 ctx 的
  `store.Save` / `s.Update`。需要把原 owner ctx 贯穿 codec adapter，调用 health 的
  `SaveEntriesContext` / `UpdateDataContext`（可增加仅转码的 SaveContext/UpdateContext）。
  不允许为了接 ctx 把业务复制到 adapter。
- 验证要求：真实 Runtime.Start/BindRequest/Shutdown，屏障分别位于共享叶层写临时文件后、
  最后 rename 前、删除前；关机主文件/备份/签名不变，临时文件清理，断开请求写/删继续。
  两种 Write 模式都测，rename 成功后关机的正控不误报失败。discovery 两条生产入口各跑
  一条跨层回归，health/check、jsonstore、discovery 合并树定向 race。
- 本次证据是调用链与叶层源码；最终 rename 窗口的确定性失败回归归后续任务，未冒充已执行。

## 草稿和作者更正审计

逐项对照 Python `backend/utils/{health_check,domain_probe,region_check,node_dns,node_latency}.py`
及迁移前 Go 测试；同时参照 discovery `778442e4d` 的类型边界标准。

| 区域 | 独立结论 |
| --- | --- |
| `review_c16_test.go` | EISDIR 取代 Python OSError 文本有原生 jsonstore 依据；空/非空目录都不得被删、缺失文件成功的断言保留。断开完成断言只改格式。 |
| `recorded_test.go` | stdin 改为完整 JSON 值比较符合 D2；argv、超时预算、环境、状态、文件检查保留。原 2280 条 suite 不走 typed 豁免；本次不重跑这整套。 |
| `corpus_test.go` | 原 790 条与两个录制文件字节未改；草稿分类器已删除。仅索引命中的精确参考替换期望，函数名、源 SHA、覆盖条数均校验。已有 Turkish-I/N2 裁定没有新增通配。 |
| `difftest/diff_test.go` | 唯一 UTF-8 差异分别断言双方完整 400 错误体，且要求命中恰好一次；stdin 只容许 JSON 编码变化。存储/system/其他响应字段仍比较。 |
| typed fixture | 业务分支、有效配置、来源身份保留；非法身份从真实存储行导入，不通过 writer 构造非法种子。`RegionResult{ID: ...}` 原生构造不依赖 hasID 私有位。 |
| model / native | 核心无 jsonx/pystr；宽松文本通过 model.String/Int，StrictString 与正整数 token 检查是显式请求/worker 协议校验，不是私有 Python scalar decoder。NullableText 仅包装 shared codec 的 null 语义。 |
| 转换和 panic | business 在 native conversion.go，legacy 只转码相邻回调；逐项 recover 后 worker 继续消费20项队列，不因全部 panic 使生产者阻塞；固定日志不输出 panic 值。 |
| 存储与 R5 | 非法 checked_at 不让有效兄弟项随整个 map 丢失；显式非法写入仍拒绝，非零 typed 修复由 model provenance 决定。新增 region 兄弟测试保留 services/domain/website。 |
| 输出与凭据 | worker stderr 和原输入均不放进用户错误；typed latency 保留未消费扩展。此次取消错误不回显子进程或节点凭据。未添加认证入口或白名单。 |

作者11项更正的业务依据成立；上述生命周期包装层和删除入口是本轮新增补漏。
未改变 Python 比较器、baseline 或既有 fixture 来迁就实现。共享 model 的既定 R5/R8、D2
和 UTF-8 原生诊断差异沿用作者明确登记，不新增行为豁免。

## 162 条参考的实际覆盖与负控

在 `.32` 用真实 Python 重跑 `n89_reference.py`，输出
`typed reference expectations: 162`，生成文件与提交版本逐字节一致。
生成器只读合成源 corpus，投影共享 schema 后调用真实 Python verdict/target/confidence
业务；没有读取 Go 输出，没有用分类名代替结果断言。

`TestReferenceCorpus` 790行/0差异，精确索引66条；`TestReviewCorpus` 1017行通过，索引96条。
进一步只把 **corpus row745 的精确期望 verdict** 临时改成 `synthetic-negative-control`，
实际 `go test -run '^TestReferenceCorpus$'` 返回失败且包含该行的 native reference mismatch；
随后 finally 恢复文件原字节。不是仅测试一个孤立比较 helper，也没有构建逐模块 mutation gate。

## 定向验证命令与末行

全部固定 `RT_HOST=sunbroke@192.168.0.32`，`GOMAXPROCS=4`，Go 命令 `-p 4`。

初始失败回归：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/health/review_n89_test.go -- bash -lc 'cd server && gofmt -w internal/health/review_n89_test.go && go test -p 4 -race -count=1 -timeout=3m -run "TestReviewN89" ./internal/health'
```

返回1；两种删除和实际 latency worker 取消失败如 M1/L1。第一次为 check 另加
Runtime 测试时曾因同包引入 lifecycle→repo→check 形成 test import cycle；已改为外部
`check_test` 包，之后正常编译通过；未把这次 setup failed 记成产品故障。

修复后的验证（退出0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/health/check/review_n89_test.go --back server/internal/health/review_n89_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/health/check/review_n89_test.go internal/health/review_n89_test.go; test -z "$(gofmt -l internal/health/)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m -run "TestReviewN89|TestExecRunnerUsesDetachedRuntimeGeneration|TestConversionPanicsDoNotStrandBatchOrPublishPanicText|TestServiceMapEncodesNativeConstructedResults|TestMalformedProbeTimestampDoesNotDiscardValidSiblings|TestStoredHealthUsesSharedNumbersWhileRequestsStayStrict|TestR5FallbacksAndStoreValidation|TestReviewStoreDeleteOfADirectoryFailsLikeOSRemove|TestWorkerFailureRequiresTextAndRetainsIntegerPrecision|TestCheckCoreHasNoLegacyOrUntypedDependency|TestHealthCoreHasNoLegacyOrUntypedDependency" ./internal/health ./internal/health/check; go test -p 4 -race -count=1 -timeout=5m -run "Latency|DNS" ./internal/api/nodes; go test -p 4 -race -count=1 -timeout=5m -run "Probe|Region|Shutdown" ./internal/discovery'
```

```text
ok configflow/internal/health       4.190s
ok configflow/internal/health/check 1.058s
ok configflow/internal/api/nodes   10.740s
ok configflow/internal/discovery    5.664s
```

gofmt、全仓 vet 无输出。另一次窄范围 race 首轮通过 health3.179s/check1.018s，
之后补了最终 mutator 检查与 DNS 写入口，以上最后运行已覆盖它们，不重复整包。

参考与互通抽检（退出0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; go test -p 4 -count=1 -v -run "^(TestReferenceCorpus|TestReviewCorpus|TestNativeReferenceChecksValuesAndSiblings)$" ./internal/health; go test -p 4 -race -count=1 -run "TestNativeProbeAndRegionFilesInteroperateWithPython" ./internal/health/check; python3 - <<'"'"'PY'"'"'
import json,os,pathlib,subprocess
p=pathlib.Path("internal/health/testdata/n89_expected.json")
before=p.read_bytes()
try:
 env=dict(os.environ,PYTHONPATH="..")
 subprocess.run([str(pathlib.Path.home()/"cf-test/.venv/bin/python"),"internal/health/testdata/n89_reference.py"],env=env,check=True)
 assert p.read_bytes()==before,"Python reference not reproducible"
 refs=json.loads(before)
 row=next(x for x in refs["expectations"] if x["file"]=="corpus.json" and isinstance(x.get("out"),dict) and "verdict" in x["out"])
 row["out"]["verdict"]="synthetic-negative-control"
 p.write_text(json.dumps(refs,ensure_ascii=False))
 result=subprocess.run(["go","test","-p","4","-count=1","-run","^TestReferenceCorpus$","./internal/health"],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True)
 assert result.returncode!=0 and "synthetic-negative-control" in result.stdout and "native reference corpus.json row" in result.stdout,result.stdout
 print("indexed reference negative control: rejected changed verdict at corpus row",row["row"])
finally:
 p.write_bytes(before)
PY'
```

```text
ok configflow/internal/health       1.818s
ok configflow/internal/health/check 1.051s
typed reference expectations: 162
indexed reference negative control: rejected changed verdict at corpus row 745
```

没有新增 fuzz target，不重复作者两个60s fuzz；本次不跑作者整套 race/44请求 Flask，
只读其证据并抽检作者风险用例、真实 Python 文件互通与精确参考。完整门禁由协调者负责。
