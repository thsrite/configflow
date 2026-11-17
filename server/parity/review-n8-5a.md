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
  修复提交：`219035cd4`。

- **F2，high，已修复：启动自动写入重新造成 R5 加载锁死。**
  `validateStored` 已允许畸形成员，但 `openLocked` 无条件运行 legacy token 初始化。
  `system_config` 是 null/数字/字符串时得到 AttributeError；配置名错误且缺 token 时，
  自动事务触发全量写入校验，`Open` 仍返回“配置名称不能为空”。四个场景修复前均失败。
  现在只有当前业务文档有效时才做可选启动写入；无效文档直接保持只读加载，修复事务再维护 token。
  `TestReviewN85InvalidStoredValuesDoNotTriggerStartupWrites` 对全部 meta/sections/fields
  和完整非 DB 文件树断言加载零改动、typed 修复可提交、无关文件保留。
  修复提交：`9ead8212a`。

- **F3，low，已修复：View / 校验文档过度承诺。** 文档称“每个 head 只解码一次”、
  “不取锁”，但已有缓存上限会让超大文档每次解码，并发冷读也不保证唯一实例；
  失败读取可能在 L3 下重试。现明确缓存命中共享、无指针身份保证、回调本身不额外持锁，
  区分事务/WithReadSnapshot 继承的锁，补充 Issues 的解码时诊断语义。
  `TestReviewN85ViewRespectsCacheLimit` 固定两次 View 的加载计数及不缓存边界；
  `ValidateDocument` 注释也不再误称每次 load 必须通过业务校验。
  三条设计 Clarified 已写入 `n7-n8.md`，并补上 Clear 的 R3 与业务维护例外。
  修复提交：本文件同提交（最终交回补 hash）。

- **F4，high，已复现，合入阻断：仍缺 O3-6c 的关机提交屏障。**
  这不是 N8-5a 新引入的取消策略，但其新 typed Update 同样受影响。
  真正 Runtime 的三组断开均成功；进入 owner 前关机被现有 acquireLock 拒绝；
  updater 内、Store.BeforeCommit 内关机都返回 nil 且 DB 已改变。
  依任务指明的双方边界，不在此分支抢改 O3-6c 正在修的共享提交核心。
  `progress/n8-5a-shutdown.patch` 保存可应用的六场景 typed 回归（外部测试包，无生产数据）；
  当前直接执行它，两个 shutdown 场景失败、四个正负控通过。
  O3-6c 合入并移植后必须应用此补丁重跑；不能用本分支绿色 gate 宣称取消问题已解决。

## O3-6c 冲突与集成要求

N8-5a **确实改了提交核心的表示和函数布局**，不是仅新增包装。相对基线，原
`repository.go:update/writeLocked` 已移至 `legacy.go:update` 和
`transaction.go:transact/transactLocked`，`writeOptions` 也已搬家；`store_rows.go:commitStore`
的文档参数变为 `*model.Document`，`openLocked` 调用改名。
本地 `go-o3-6c` 当前为 `92728a22f`（任务给出的 `6e5c7760` 已不在本地 refs），
其核心提交 `2697f0b1d` 改的是旧函数布局。

合入 O3-6c 后移植清单：

1. `writeOptions.batch` / `beginCommitBatch` 放到 transaction.go；`transact` Detach 后检查
   ctx.Err，去掉对全部 updater 的自动 Uninterruptible。
2. `writeLocked` 的检查迁入 `transactLocked`：进入前检查；只有批准且已打标的本地文件批次
   在 fn 返回后 Uninterruptible；其余工作 context 一直传到最终提交。
3. `commitStore(ctx, base, *model.Document, meta)` 保留 typed `encodeModelRows`，
   使用 O3-6c 的 `Store.CommitContext`，包括 BeforeCommit 返回后、紧邻 COMMIT 的取消检查。
4. 保留其 DeleteProfile、ImportAll/ResetAll、CreateRuleLibraryItem 的 batch 打标和
   文件提交检查，保留缺 JobLock 的 fail-closed；更新 openLocked 及其他调用点。
   N8-5a 对 profiles/shared 的 merge 调用改动与 batch 位置不同，仍需逐段核对。

## 六项重点结论

1. 新写入口仅 `Update`，旧写 API 均通过 legacy.update → transact；Clear 适用于全部 record。
   null/畸形 fallback 置 nil 保留、同一行其他成员修改保留、非零 replace、显式 Clear 删除，
   加上 F1 修复后通过。有效非 nil 指针置 nil 仍按 R3 删除，不应误写成“一切 nil 都 untouched”。
   token 两字段为既有业务维护，会生成/去重；Clear 不绕过必需字段校验。
2. Nodes 比较按类型且同类型数字按精确值；独立表验证不同文本/顺序不覆盖并发修改，
   大浮点真实变化不会被 float64 舍入吞掉。没有新增冲突错误，沿用 Python 的 delta/覆盖规则，
   另由 DB CAS 与 section revision 处理事务冲突。30 例 Python 参照的 3 例类型差异已明记。
3. View 只读契约成立；仓内生产使用仅 Profile/Shared/System 三个复制读取包装，
   未发现非测试调用方保留可变引用再写回。事务工作副本从 immutable rows 独立解码，提交再解码，
   不与缓存共享。已有 race/WithReadSnapshot/嵌套写拒绝测试覆盖锁关系。
4. L2/L3 层次及缺 JobLock 的 fail-closed 没有放宽；中途关机仍为 F4 集成阻断。
5. prerequisite 测试已重命名并覆盖五组 load/write/repair、原字节、typed/legacy 读；F2 补齐启动分支。
6. legacy.go 是过渡转换层（另含尚待 N8-5c 迁移的 token 桥）；没有新增业务调用方绕开 typed
   事务核心。N8-5b/c 的旧业务文件仍在原文件中，不能把 a 段宣称为整个 repo S pass 完成。

## 当前验证

测试服务器 `.64`：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD"); go test -count=1 -timeout 20m -run "Test(TypedUpdateStoredRoundTrip|ModelR5StoredValuesLoadWriteValidationRepairs|MergeJSON|ClearMember|ReadsInsideTransactions|CancellationReads|TypedReads|View)" ./internal/repo ./internal/model'
```

repo `20.016s`、model `0.007s`，通过，含真实 Python 存储审计。
新增回归：`go test -count=1 -run '^TestReviewN85' ./internal/repo`，F1 修复后 `ok 0.761s`。
F2 与既有 startup/R5/invalid-row 回归联跑：`go test -count=1 -run '^(TestReviewN85|TestStartupEnsuresTheRuleProxyToken|TestInvalidRowsLoadButRefuseWritesWithoutRebuilding|TestModelR5StoredValuesLoadWriteValidationRepairs)' ./internal/repo`，`ok 5.914s`。
Nodes 新增独立表覆盖同类型不同数字文本、对象顺序/转义、大浮点精度、同成员覆盖、删除、不同类型 id。
`TestReviewN85PythonFullStorageDifferential` 从真实 Python 创建并写入的合成 DB 复制出两侧，
依次执行 rename/same-row/replace/clear；每步比较全部 DB 三表的每个列（仅解析 fields.value
消除 JSON 排版差异），完整非 DB 文件路径/内容，以及完整 export；无成员或目录宽泛排除。
覆盖错误类型的 description/auto_backup、default_profile_id null、未知根/嵌套字段、合成 JSON 文件。
命令 `go test -count=1 -run '^TestReviewN85PythonFullStorageDifferential$' ./internal/repo`
（CONFIGFLOW_PY/CONFIGFLOW_REPO 同上，`.64`），`ok 6.090s`。
新增 fuzz `FuzzMergeJSON` 检查有效 JSON、输入不变性、未修改 baseline 时精确保留 current 字节。

## 待完成

- 完整 gate、Python 全 repo/model race、fuzz；最终提交列表。
- O3-6c 集成取消修复是明确的合入前置，协调者负责集成后复验。
