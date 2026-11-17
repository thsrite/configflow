# A13 对抗式复核

独立 Codex 复核，非原作者；分支 `go-review-a13`，起点 `go-port-a13@0de43b03`。
范围：content sync Checker、缓存与失效广播、四个 GET 路由；非 HR。
无子 agent、无 push/PR、无生产访问，测试经 rt 在测试服务器执行。

状态：**复核中**。F1 已修复并经 race 回归；F2 已复现，等待 A24 共享修复进入 go 后变基接入。

## 发现

- **F1，中，已修复：真实 Spawn 丢弃 owner 取消，关闭后任务继续准备。**
  `Checker.Close` 取消 reservation context，但 `Runtime.Spawn` 明确使用 WithoutCancel，
  回调收到的任务 context 不再由该 reservation 取消。正在准备的任务继续 I/O，
  已准入但尚未开始的任务甚至在 Manager 关闭后才调用 Prepare。
  原延迟替身直接转交 context，掩盖生产差异；cache/busy 清理成功不等于任务真正停止。
  回调现将 reservation 的取消重新连接到 Runtime 提供的 task context，保留新 scope、
  ClockFrom 和 shutdown cancellation；启动前已取消时同步拒绝准备。
  `TestReviewCloseCancelsRealRuntimeWorkers` 使用真实 Runtime，分别覆盖运行中及启动前关闭，
  同时断言 WaitIdle、槽位、Keyed reservation、状态及详情全部清理。
  修复前两场景均失败；修复后 contentsync race 通过。
- **F2，高，待修复：环境注册密钥在 diff/entries 及搜索中泄露。**
  `StoredSecretFinder` 只读取仓库 tokens，遗漏 `AGENT_REGISTRATION_KEY`。
  明文、URL 编码及跨 CJK 的局部文本能从 push-diff、entries 返回并被 q 命中。
  真实 Python 同样复现，属于安全修正；协调者已确认在共享 finder 纳入环境密钥，
  保持全局 JSON hook 和用户可 reveal 的配置凭据策略不变。

## 补充检查

`TestReviewProductionCacheSpec` 直接钉住 New(Options{}) 的生产 spec：2 worker、900/30 秒、
512 状态、8 详情、48 MiB；未把这些默认值替换成缩小的测试参数。
实际创建 514 个状态和 9 份详情，并检查 48 MiB 包含边界及超限后最旧详情淘汰。
`TestReviewUnregisterWaitsForBroadcast` 在真实重叠的广播与注销间检查同步和后续隔离。

## 当前命令与证据

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -run TestReview ./internal/deploy/contentsync'
```

修复前：

```text
before-start=false: Close did not cancel real Runtime preparation
before-start=false: closed Checker left Runtime busy
before-start=true: closed Checker left Runtime busy
before-start=true: closed Checker started preparation after admission
FAIL configflow/internal/deploy/contentsync
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/deploy/contentsync/review_test.go -- bash -lc 'cd server && gofmt -w internal/deploy/contentsync/review_test.go && go test -race -count=1 ./internal/deploy/contentsync'
```

```text
ok configflow/internal/deploy/contentsync 5.240s
```

后续追加生产边界/并发广播后，同包 race：`ok configflow/internal/deploy/contentsync 2.531s`。
F2 真实 Python oracle 由 `testdata/review_a13_reference.py` 调用完整准备与 Flask 路由：
`1 passed in 1.56s`，固定输出 `Python reference: registration key plaintext/encoded exposed in diff, entries and search; confirmed`。
Go HTTP 相反断言先失败，确认明文和编码形式都能泄露/搜索。

剩余：合入 A24 共享修复、修 F2、完成共享脱敏/分页与 20 条 baseline 因果证明复验，
`git rebase go` 后运行 `server/scripts/gate.sh -p 6` 和相关 fuzz；零未决后改 verified。
