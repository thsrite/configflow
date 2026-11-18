# C06 / C11 交回

分支：`go-port-c06-c11`，基线 `go@f90a156e`。不 push，不碰 C07，不启动子 agent。

## 实现提交

- `defade57d`：C11 复用 `rules.Regex.Groups()`，删除私有捕获组扫描；五类正则的真实 Python 对照、完整 provider 输出和 `$N` 排序替换；逻辑链引用及无 provider 时不提前校验的回归；逐源用例映射。
- `f12dfaf9e`：C06 六路径 HEAD 只读、所有自有 body reader 的 Bind/BindSilent、原有校验兼容 DTO；完整 DB/文件差分、脱敏 Python 对照、全部已注册路由认证矩阵及源用例映射。
- `63411a71c`：O3-6a 合入后，C06 保存及备份 owner Detach/关机检查；8 类写操作、16 个真实 Runtime HTTP 回归。沿用现有 repo Scope/baseline、共享锁和 WebDAV owner。

两模块均为 `ported`，没有由作者标记 `verified`。协调者安排两个独立、非作者 Codex 复核。

## 验证

全部通过 rt 在 `.64` 执行；精确命令及完整队列守卫见 [C06 记录](progress/c06.md)，C11 映射见 [C11 记录](progress/c11.md)。

- 132 个真实 Flask 请求/完整 SQLite meta、sections、fields 及文件比较；六路径 HEAD 逐条比较 GET headers、DB 指纹（含 revision）、所有文件。
- 五类正则真实 Python 对照、完整 provider 配置和排序替换；C11 生成消费者及现有 corpus 通过。
- 8 个原始 Python 测试文件：`296 passed in 44.94s`。
- 最终 owner 定向：16 个 Runtime 测试通过，3.370s。关机后不写 DB/文件且无 WebDAV 请求；仅客户端取消则设置、远端目录、完整上传及成功回执均完成。
- 最终 `gofmt -l internal/ tools/ cmd/` 无输出，`go vet ./...` 通过。
- 最终定向 race：`api/settings 29.111s`、`httpx/difftest 69.990s`、`subs/difftest 21.301s` 全通过，含当前合入的 C29 注册。

## 完整 gate：排队上限交回

前台执行 `server/scripts/gate.sh -p 6 FuzzBindNativeJSON:./internal/httpx FuzzSubscriptionYAMLReaders:./internal/subs`，外层仅增加 900 秒队列守卫。
两个名额持续占用；900 秒仍未取到 slot，守卫终止本次排队，rt 返回 `75`。依协调者要求直接交回，不重新排队。

**完整 gate 的所有检查阶段及两个 60 秒 fuzz 均未执行**；不把定向通过记成完整门禁通过。
日志目录：`.64:/home/sunbroke/.cache/rt-tmp/c06-c11-final-20261118`。

## 差异与边界

只有已批准的 HEAD 只读修复、N8-6 严格绑定 400/415 及非有限 JSON 拒绝差异；均有显式真实 Flask 预期差异。未发现其他本模块行为差异。

**已知、已分派：O3-6c / 6e5c7760** 处理 repo 最终 COMMIT、taskstatus 带 ctx 入口/调用方、rulefetch touch/rename 及共享 archive 链。特别是当前 `webdav.Run` 在 Upload 返回后调用无 ctx 的 taskstatus.Record，执行中关机仍可能写失败回执。C06 已在进入共享层前检查；没有声称共享窗口已关闭，也没有复制共享 helper。

外部入口保留原 owner：finish 的 MCP、C34 config/export/generate HTTP、C33 proxy-group HTTP；精确缺项与已有契约证据均在逐项映射中。N8-5 的 typed repo 消费者迁移位置也已列明。


## 独立复核最终交回（以上为作者原始历史）

分支 `go-review-c06-c11`，最终基线 `go@a9d6ce86c`。两模块均由非作者设为 verified：
- C06：1 medium 日志凭据/文本泄露，已修 `8168bc648`；新增 34 个存储边界请求 `4cbbb19f9`。
- C11：1 medium 验收覆盖 + 1 low 计数问题，已修 `118f1084c` / `56509f415`；13 个真实 Python regex 向量。
- 详细场景、证据、修复及外部 owner：[review-c06.md](review-c06.md)、[review-c11.md](review-c11.md)。

`.32` 完整 gate PASS：全量 race（95 个 ok 包）、15 个启用 difftest、离线 new=0/stale=0、
两个 60s fuzz 全通过。其后合入 C33/YAML 复核再变基，相关定向 race/完整消费者、全仓
vet/格式全部通过。两轮证据分开记录，不把变基前 gate 写成最终整树 gate；按新 WORKFLOW
由协调者对 batch 树统一验收。原始 8 个 Python 文件另有 296 passed。
共享 O3-6c、MCP/C34/A14 和 C33 的责任边界不由本复核冒认完成；C33 已合入且其自身复核见 review-c33。
未 push、未访问生产、未启动子 agent。工作区最终应为干净，最终文档提交见分支 HEAD。
