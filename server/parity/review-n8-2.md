# N8-2 / A07 代理差分对抗性复核

状态：进行中；分支 `go-review-n8-2`。作者之外独立复核。最终 .32 验收后才标 verified。

## 发现与修复

| 严重程度 | 发现 | 修复 / 边界证据 |
|---|---|---|
| medium | store.Commit 提交完全相同的 raw metadata 仍执行写入，generation 的空白格式也会被重写 | 过滤 byte-identical metadata；meta-only 写入不改无关 generation。新 no-op 回归先复现失败，再修复 |
| medium | decodeError 模拟 sqlite3 异常并回显整个非 UTF-8 行，可泄露 token、密码等任意字段 | 删除模拟；报表/键/列、一基字节偏移、错误类别。非 UTF-8 统一 ErrCorrupt；坏 JSON 使用 encoding/json SyntaxError.Offset；不打印 value |
| medium | 坏行启动失败缺少完整原路径/可执行恢复说明，错误只到 stderr | repo 原文件路径及停止服务/保留原文件/备份恢复/最后可读版本导出提示；最终 startup 边界 logx 同时输出日志和 stderr |
| low | storage_version 的错误诊断回显值，超大数用 big.Int 解析并整段输出 | 原生 strconv.ParseInt；错误类型提示无 value，超大正数给有界的 unsupported 提示 |
| medium/security | N2 简化环境选择器遗失 CGI 的 httpoxy 防护；原 differential 28 次请求有 5 处失败 | 改共享 httpproxy，CGI 不发请求。loopback 直连和其余原生差异经协调者明确接受；保留 ALL_PROXY 用户功能 |

## 范围与接受差异

- 设计 §2.4 的原始行、schema/version、row positions、revision、SQLite 原子事务保留。
- **语法损坏** JSON / 非 UTF-8：依 C01/C03 决定拒绝启动并保全原库，不自动重建。
  合法 JSON 的错误类型 R5 保留/读零/单次 warning 属 N8-3，不在临时桥里另造模型。
- **repo.update 入口 detach（PORTING 4b）→ N8-5/O3-6**。协调者明确本轮不提前迁移。
  store API 没有请求 ctx，底层事务用 background；新测试在 BeforeCommit 阻塞时取消
  发起请求，随后放行，确认整次提交成功。外层等锁取消行为仍由后续步骤迁移。
- 指针身份、Python 内部异常文本不追；原始行字节、未知成员、顺序、no-op head 与
  revision 必须保持。语法损坏 UTF-8 改统一 ErrCorrupt，是安全诊断分类变更。
- 代理决定及恢复配置方式见 behaviour-changes.md 的 N2 记录。旧 A07 决定 5 失效；
  httpproxy 使用已有 x/net 依赖。ALL_PROXY 仅作 Config 的空 scheme 回退，其余选择、
  CGI、NO_PROXY、解析均交原生库，未保留自写 matcher 或 Python 代理模拟。

## 覆盖与未完成验收

已逐分支对照 backend/common/config_store.py（document_rows/build_document、读快照、
metadata/head、diff、commit/apply、build/install/check/salvage/backup），Go store 与
临时 repo/store_rows.go 桥；读取作者的 N8-2 记录及现有跨语言测试。

- 保留作者 TestStoredRoundTrip（12+12+私有库43行）、真实 Python ConfigStore 交叉检查、
  跨进程并发/冲突、失败回滚、backup、field order/revision 测试。
- 新增 store/review_n82_test.go：完全相同 metadata/no-op；7 种坏行（含 secret、截断、
  非 UTF-8、metadata、超过 JSON 最大深度、10 MiB 损坏行）；约9 MiB 合法 opaque 行
  原样读写与8-writer单赢家；取消后完成；100k-digit version 有界诊断。
- repo/review_n82_test.go + testdata/review_n82_interchange.py：私有只读备份、真实
  Python ProfileRepository 和 Go Repository 双向 profile 写入、无改动提交；未知
  坏行启动的定位/恢复/不泄密，以及原 DB 文件逐字节不变。
- agents/proxy_diff_test.go：真实 requests / Go、loopback proxy/Agent +仅 DNS 映射的
  synthetic routable host。14环境×3目标，18 个明确原生结果差异、24 个相同结果，分别断言完整 target、Host、代理认证和
  Authorization；CGI 为错误且0请求。无 mask/跳过差异。
- Agent / Sub-Store 新测试：ALL fallback、scheme 优先、小写优先、CGI/ALL 不绕过；
  旧 loopback 回归改为直连，Sub-Store 显式代理、proxy→direct redirect 仍实测。

迭代用 .63；需要真实代理 differential 和私有快照在 .32。最终命令/尾部输出待补。

已通过迭代：.63 `go vet ./...`；真实 Python 环境下 store 12.910s、repo 49.691s、
weburl 0.003s、agents 50.050s、substore 8.168s。`.32` 原始 store 往返 2.015s，
新私有 repository 双向写入 1.560s。一次新测试回调误把 hosts 当对象而 panic，已改用
mihomo 中的合成未知成员后通过；这是测试构造错误，无生产代码修复或私有内容输出。

代理最终迭代：.63 vet/agents 41.321s、substore 6.481s、weburl 0.003s；.32 42请求
差分 **18 个显式结果差异 / 24 相同，4.511s**。3种合成坏库的真实CLI启动：退出1，
日志/stderr均有安全恢复提示，原数据库逐字节不变；即使 LOG_LEVEL=CRITICAL 也可见。
