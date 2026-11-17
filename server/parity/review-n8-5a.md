# N8-5a 独立复核

状态：复核进行中；分支 `go-review-n8-5a`，起点 `go-n8-5@fbe61bc64`。
复核者未参与原实现；无子 agent、无 push、无生产访问，用例仅含合成数据。

## 发现

- **F1，high，已修复：自动 token 维护破坏未改成员的原始字节和值。**
  `ensureLegacyRuleProxyToken` 将整个 legacy `system_config` 重新解码并覆盖 typed record。
  retired token 去重或 token 轮换就会丢弃其他成员的 provenance；typed 和 legacy 事务均受影响。
  合成回归同时观察到畸形 `node_test_dns` 内 `1.2300` 被规范化，未知成员的
  `9007199254740993.0` 变成 `9007199254740992.0`、`\u003c` 变成 `<`。
  修复只回填业务维护实际拥有的 `rule_proxy_token`、`retired_rule_proxy_tokens`，保留其他字段的 Raw。
  回归 `TestReviewN85TokenMaintenancePreservesUntouchedMembers`：修复前两入口均失败，修复后通过。
  修复提交：本文件同提交（最终交回补 hash）。

## 当前验证

测试服务器 `.64`：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD"); go test -count=1 -timeout 20m -run "Test(TypedUpdateStoredRoundTrip|ModelR5StoredValuesLoadWriteValidationRepairs|MergeJSON|ClearMember|ReadsInsideTransactions|CancellationReads|TypedReads|View)" ./internal/repo ./internal/model'
```

repo `20.016s`、model `0.007s`，通过，含真实 Python 存储审计。
新增回归：`go test -count=1 -run '^TestReviewN85' ./internal/repo`，修复后 `ok 0.761s`。
Nodes 新增独立表覆盖同类型不同数字文本、对象顺序/转义、大浮点精度、同成员覆盖、删除、不同类型 id。

## 待完成

- 启动时自动 token 写入是否绕过加载/写入校验拆分。
- View 的缓存/生命周期文档核实；三条 Clarified 写入设计。
- O3-6c 提交核心冲突及取消边界清单；完整 gate；最终提交列表。
