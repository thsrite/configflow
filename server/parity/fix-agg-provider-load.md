# Aggregation provider 高负载差异排查

分支 `go-fix-agg-provider-load`，从当时最新 `go@df6928ae` 建立。状态：**已完成，零未决问题**。修复已迁到 O3-5 `go@9a208c6c`，完整门禁和受限 CPU 复验均通过。无子 agent、无推送、无生产访问；所有执行均通过 rt，真实快照及原始响应只留在测试服务器的私有目录，测试只用合成数据。

## 根本原因

这是**中等严重程度的验收不稳定问题**，没有发现新合并引入的 provider 功能回归。Python 与 Go 原有生产合同相同：Mihomo 格式等待 12 秒；渲染未完成且存在旧文件时，返回旧 YAML，并带 `X-ConfigFlow-Provider: last-good`，后台继续生成；无旧文件则继续等待；完整的新结果不带该 marker。Surge/Loon 不降级。

之前的差分同时比较两个独立调度的请求：Python 在负载下超过预算，Go 已渲染完成。因此 missing-in-go 表示 Python 返回了降级 marker，而不是 Go 丢失 marker。proxies 长度差异的 Python body 已在服务器上严格比对，**与原始快照的一份 provider YAML 完全相等**；Go 的新结果过滤/选择后节点更少，并非无标记地提前降级或丢节点。请求重试只解决传输失败，两个 HTTP 200 的不同新旧版本不会被它消除。

`git diff 65c0e6d4 df6928ae -- server/internal/provider/response.go backend/utils/provider_response.py server/tools/proddiff/ server/cmd/configflow/manifest_proddiff.go` 为空。旧版本本身也可复现 header 差异，所以不能归咎于 A24、A12 或 N8-4 的某条新增 provider 语义。负载改变每个请求越过 12 秒边界的时机。

## 同机版本对比（.64）

两次测试都原样运行 `server/scripts/proddiff.sh`；工具自己的私有运行锁将比较阶段串行，使用同一快照、同一台机器。

| 版本 | 条件 | 工具记录的开始→结束 1 分钟负载 | Python/Go 传输重试 | 结果 |
|---|---|---|---|---|
| `65c0e6d4` | 原样 | 42.18→37.95 | 0/0 | new=0 stale=0 |
| `df6928ae` | 原样 | 40.89→36.10 | 0/0 | new=0 stale=0 |
| `65c0e6d4` | nice 10、固定 CPU 0 | 22.40→27.37 | 2/0 | new=1，profile provider header |
| `df6928ae` | nice 10、固定 CPU 0 | 21.48→28.22 | 2/0 | new=3，原报三条完全复现 |
| O3-5 + 修复 | nice 10、固定 CPU 0 | 31.19→26.02 | 1/0 | new=0 stale=0，覆盖回退 0 |
| O3-5 + 修复 | gate 内与全量 race 并行 | 42.70→35.94 | 0/0 | new=0 stale=0，覆盖回退 0 |

```sh
# 基线在私有 detached worktree ../go-agg-provider-baseline；当前分支在本 worktree。
git worktree add --detach ../go-agg-provider-baseline 65c0e6d4
RT_HOST=sunbroke@192.168.0.64 rt -C ../go-agg-provider-baseline -- bash -lc 'cat /proc/loadavg; server/scripts/proddiff.sh > /tmp/agg-load-baseline.log 2>&1; code=$?; rg "^self-check:|^baseline:|^proddiff:" /tmp/agg-load-baseline.log; cat /proc/loadavg; exit $code'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cat /proc/loadavg; server/scripts/proddiff.sh > /tmp/agg-load-current.log 2>&1; code=$?; rg "^self-check:|^baseline:|^proddiff:" /tmp/agg-load-current.log; cat /proc/loadavg; exit $code'
RT_HOST=sunbroke@192.168.0.64 rt -C ../go-agg-provider-baseline --no-sync -- bash -lc 'cat /proc/loadavg; nice -n 10 taskset -c 0 server/scripts/proddiff.sh > /tmp/agg-load-baseline-cpu1.log 2>&1; code=$?; rg "^baseline:|^NEW" /tmp/agg-load-baseline-cpu1.log; cat /proc/loadavg; exit $code'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'cat /proc/loadavg; nice -n 10 taskset -c 0 server/scripts/proddiff.sh > /tmp/agg-load-current-cpu1.log 2>&1; code=$?; rg "^baseline:|^NEW" /tmp/agg-load-current-cpu1.log; cat /proc/loadavg; exit $code'
```

```text
baseline: new=1 stale=0
NEW GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider header.X-ConfigFlow-Provider missing-in-go

baseline: new=3 stale=0
NEW GET /api/aggregations/<agg_id>/provider body.proxies length
NEW GET /api/aggregations/<agg_id>/provider header.X-ConfigFlow-Provider missing-in-go
NEW GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider header.X-ConfigFlow-Provider missing-in-go
```

## 修复与回归

只固定比较时的等待条件。Python 专用 launcher 将 `Future.result(timeout=...)` 的预算设为 `None`；Go 仅在 `configflow_proddiff` 构建的启动钩子里设置负预算，等待结果。生产默认仍为 12 秒，缓存读取、provider 生成、原子发布、last-good 保存及响应复制保持原合同；没有改 header/body，也没有跨端注入内容。

- `TestSlowProviderParity` 对全局/profile 两个路径比较真实 Python 公共 provider owner：生产模式用真正 12 秒预算和同步屏障，验证旧 body + marker，及随后后台写入新 body；比较模式验证新 body + 无 marker，仍保存新产物。修复前 fresh 两个用例均失败，修复后 .32/.64 race 均通过。
- `TestProddiffWaitsForFreshProvider` 用真实 tagged 启动钩子和阻塞 render 验证比较进程确实等待。
- 比较器新增负向控制，两条 provider 模板上的 marker 缺失和 proxies 长度差异仍必须报错。没有修改 comparator、normalize、baseline 或覆盖下限。

O3-5 集成保留新的 `Flight.DoChan`、Runtime 所属工作、lifecycle Clock 和可取消的 waiter；只在 `awaitResponse` 创建 fallback timer 的条件加 `seconds >= 0`。请求取消/Runtime 关机仍走 O3-5 原路径，未恢复旧的 bare goroutine 或不可取消等待。

最终门禁、额外负载验证和交回信息见后续各节。

O3-5 合并树专项复验（.64）命令与末尾输出：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/provider/response.go -- bash -lc 'cd server && gofmt -w internal/provider/response.go && CONFIGFLOW_DIFF=1 go test -race -p=4 -count=1 ./internal/provider/difftest && go test -tags configflow_proddiff -count=1 ./cmd/configflow -run TestProddiffWaitsForFreshProvider && go test -count=1 ./internal/provider -run "Test.*(Wait|Cancel|Fallback|Slow)"'
```

```text
ok configflow/internal/provider/difftest 55.085s
ok configflow/cmd/configflow 0.152s
ok configflow/internal/provider 1.030s
```

## O3-5 修复后的受限 CPU 复验

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cat /proc/loadavg; nice -n 10 taskset -c 0 server/scripts/proddiff.sh > /tmp/agg-load-fixed-cpu1.log 2>&1; code=$?; rg "^self-check:|^baseline:|^NEW" /tmp/agg-load-fixed-cpu1.log; cat /proc/loadavg; exit $code'
```

```text
baseline: new=0 stale=0
aggregation-provider: cases=2 success=2 compared=2
agent-deployment-bundle: cases=3 success=3 compared=3
coverage_regressions=0 self_check=PASS
```

额外逐对检查私有 responses.jsonl 中 5 个成功的 aggregation provider 响应：Python/Go 均为完整新结果，YAML 结构完全相等，两侧均没有 last-good marker。这个检查只输出数量/布尔值，不导出任何真实字段或产物。没有使用冻结输入，没有 partial 覆盖。

## 最终 gate

按本任务要求在 .64 使用 `-p 4`（比新版环境默认 `-p 6` 更保守）；保留服务器的并发 gate 槽位限制，不绕过队列。

```sh
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'GATE_LOGS=/tmp/agg-provider-load-final server/scripts/gate.sh -p 4 FuzzProviderYAML:./internal/provider FuzzAggregationDocument:./internal/aggregations'
```

该批次退出 0。等待服务器已有门禁释放槽位后运行，未绕过并发限制。gofmt、vet、两种 build tag 检查通过；全量 `go test -race -p=4 -count=1 -timeout=60m ./...` 共 87 个含测试包通过（真实 Python difftest 在下一阶段单独启用）。关键输出：

```text
ok configflow/cmd/configflow                  120.288s
ok configflow/internal/aggregations             1.333s
ok configflow/internal/api/aggregations         2.665s
ok configflow/internal/provider              187.248s
baseline: new=0 stale=0
== difftest
ok configflow/internal/api/agents/difftest       118.609s
ok configflow/internal/api/aggregations/difftest  21.753s
ok configflow/internal/api/subscriptions/difftest 67.279s
ok configflow/internal/devicenotes/difftest        0.567s
ok configflow/internal/discovery/difftest          2.220s
ok configflow/internal/health/difftest            13.838s
ok configflow/internal/httpx/difftest              1.744s
ok configflow/internal/mosdns/difftest             2.005s
ok configflow/internal/provider/difftest          50.060s
ok configflow/internal/refresh/difftest            3.668s
ok configflow/internal/subs/difftest               1.545s
ok configflow/internal/substore/difftest           6.238s
ok configflow/internal/webdav/difftest            16.927s
== fuzz (2 targets, 60s each, concurrent)
FuzzProviderYAML ok configflow/internal/provider 61.060s
FuzzAggregationDocument ok configflow/internal/aggregations 61.050s
GATE PASS
```

FuzzProviderYAML 执行 26,289 次，FuzzAggregationDocument 执行 22,350 次，均无失败。gate 的 proddiff：`self_check=PASS new=0 stale=0 coverage_regressions=0 unmapped_routes=0`；aggregation 2/2、Agent bundle 3/3，没有 partial。35 个工具合成回归也全部通过。前述已启用的真实 Python 慢渲染 race 单独验证生产超时语义，没有把 gate race 阶段跳过的差分测试计为该证据。

## 交回

分支 `go-fix-agg-provider-load`，修复/测试/文档均已提交，最后执行 `git rebase go`；提交 ID 见交回消息和分支 HEAD。相对 O3-5 的业务等待路径只有 `if fallback` → `if fallback && seconds >= 0`；负值仅由 proddiff 启动钩子使用。没有生产功能变更、没有新依赖、没有 comparator/normalize/baseline 改动、没有推送。临时基线 worktree 确认干净后已删除；私有原始日志和报告继续留在测试服务器。
