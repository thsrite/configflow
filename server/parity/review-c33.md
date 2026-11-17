# C33 独立复核

状态：复核进行中，缓存修复已通过定向检查；最终门禁待执行。分支 `go-review-c33`，独立于作者，
从 `go-port-c33@a4445071` 分出，首先 rebase 到本地 `go@dda8973d`，已包含 A13。
实际冲突为 behaviour-changes.md 和 proddiff 自检入口；保留双方登记/测试，main 入口置于文件末尾。
无子 agent、无 push；自写用例及 fixture 只用合成数据。所有测试/格式/vet 经 rt 在服务器前台运行。

## 发现与修复

- **F1，high，已修：失效标记检查遇权限错误时复活 legacy 缓存。**
  场景：DB/缓存编辑已有 `.invalid`，by-id 目录不能搜索，而上级 legacy 文件仍可读。
  `fileExists` 将 EACCES 当成不存在，两个读路径最终返回旧 legacy。
  修复为仅 `os.ErrNotExist` 允许回退，其他 marker 检查错误全部 fail closed；不改下载/写入协议。
  `TestReviewUnreadableReceiptCannotReviveLegacyCache` 修复前返回 `+.stale-legacy.example`，
  修复后 shared cache 读 nil，HTTP 交付 DB inline 字节。修复提交：缓存修复提交（最终 ID 在文末）。
- **F2，high，已修：交付读请求无锁补写旧快照，能越过三步提交协议。**
  场景：请求已取得旧 compat 快照；draft 提交新 DB 并 rename；标记尚未清除时请求 fallback 到旧 inline，
  再经 SaveRuleToLocal 覆盖新稳定文件；draft 随后清 marker，后续请求交付旧缓存。
  `TestReviewOldInlineReaderCannotOverwriteCommittedDraft` 在真实 rename 后执行已有快照 reader，
  不替换存储、超时、HTTP 参数。另 `TestReviewInlineFallbackDoesNotPublishOutsideEditLease` 修复前
  直接观察失效期间的 GET 重写稳定文件。
  修复：标记存在或无法确认时只交付 inline；普通冷 miss 保留补缓存，先重新读取当前声明。
  一致性守卫收敛到现有 rulefetch.WriteValidatedRuleCache：所有 inline 调用方在现有 repo 事务锁内核对
  当前 content 声明和原始字节，过期/已删除/已变 remote 的旧调用不发布；有 marker 同样不发布。
  内部 sentinel 中止未改变的 DB 候选，既不提交 DB，也不触发 hooks，不增加 API。
  调用方扫描包含 discovery、Agent 工具、compose；新增 TestReviewOtherInlineWritersCannotOverwriteCommittedDraft
  直接调用它们共用的真实 SaveRuleToLocal，验证编辑完成后迟到旧写不覆盖。
  `TestReviewColdInlineReaderPopulatesCurrentDeclaration` 验证旧请求可完成，补写字节来自新 DB，revision 不变。
  C32 原逐路由和冷交付差分保持完整文件比较，不豁免缺失缓存。修复提交：同 F1。
- **F3，medium，已修待提交：两个 reorder 入口把真值非对象丢成空对象，错误返回404。**
  `[1]`、`"bad"`、42、true 在真实 Flask 为500，Go 为404；空列表/null/false/0/空字符串原来均按空对象处理。
  已保留严格 JSON/media 边界和 falsey 默认，仅真值非对象提前返回 native500。
  `TestReviewReorderMalformedHTTPParity` 对两路由逐请求比较状态、envelope、DB/files。
  Python 内部 AttributeError 文字按既有标准不复制；只归一这两路由、合法非对象请求的已知内部诊断，
  其他字段及存储完整比较。修复提交：待下一提交。

## 缓存崩溃/故障核对

| 中断/故障点 | 安全结果及恢复 |
|---|---|
| temp 写/Sync、marker 创建/Sync、目录 Sync 失败 | DB 尚未提交；保留原声明；不清除原有失效标记；已完成 temp/marker 清理尽力执行 |
| stage 完成、DB 前 kill | DB 为旧声明，marker 拒绝所有旧文件；inline 仍可交付；下次编辑/手动缓存修复 |
| DB 未知提交错误 | failClosed 保留 durable marker，不能据内存错误误判回滚；真实 BeforeCommit 故障回归继续执行 |
| DB 后、rename 前 kill/关机 | 新 DB、旧文件被 marker 屏蔽；关机后不 rename/清理；新 generation 可修复 |
| rename 失败 | 返回500，保留 marker；移除文件尽力执行，失败也不能读旧 stable/legacy |
| rename 后、marker 前 kill/目录 Sync 失败 | 文件不作为已验证缓存交付；DB inline 可用；手动缓存重新 stage/Sync/rename/清 marker |
| marker 移除及目录 Sync 后 kill | 新文件已先持久化；重启读新缓存；若移除未持久化而 marker 重现，仍安全回退 DB |

新增 `TestReviewCacheCrashRecovery` 真实重执行测试二进制，分别在 staged/committed/renamed/complete
四个位置 SIGKILL，自另一个进程重新打开 SQLite，验证三种 HTTP 交付、失效缓存拒绝和手动修复后的
缓存字节、marker/temp 清理。没有把正常函数返回当成崩溃。磁盘满不填共享盘，按同一写/Sync错误分支审计；
实际故障注入沿用作者的写权限失败、rename失败、未知提交错误及真实 Runtime 取消测试。

## 进行中的验收

已读 Python 两路由全文、三项主测试、policy editor 内容半边、DNS follow 和 scheduled rule source 相关分支；
已核对作者交接、25方法矩阵、WORKFLOW四常设项、O3/N7/N8设计及 C29/C32/A24/N8-4ef 问题类别。
当前树无 review-c29.md，已通过 `git show a2df30f6:server/parity/review-c29.md` 完整读取独立报告。

共享 taskstatus/repo/rulefetch 的最终提交/锁等待取消窗口按明确分工仍归 O3-6c，**不是 C33 发现**；
本轮缓存 reader 的声明一致性及 marker 判定不等同于该关机窗口。不得把 C33 回归当成共享层已完成。

命令、完整测试结果、F3提交和最终状态将在门禁后补齐；不引用作者历史 GATE PASS 作为本次修复后的验收。
