# C05 对抗性复核

范围：`backend/common/profile_context.py`、`backend/common/oxidns_settings.py`，对照
`repo/scope.go`、`repo/defaults.go`、`httpx/app.go` 的 profile middleware、
`oxidns/settings.go`、`convert.AppendURLQuery`。未改公开 API。结构改造按协调者约定留给
N8 设计后的 S 轮次。当前为检查点报告；最终 `.32` 全量验收待补。

## 问题及修复

- **中：方括号主机未校验。** `AppendURLQuery` 只检查左右括号是否配对，接受
  `https://[not-an-ip]/config` 和 `https://[192.0.2.1]/config`，Python 拒绝。
  它生成的是客户端实际要请求的 URL，接受不可请求的 host 只会推迟失败。协调者明确要求
  用 Go 原生校验关闭此问题：只检查 authority，排除 userinfo，`net/url` 按既有规则处理
  zone，再用 `netip.ParseAddr` 且 `Is6()` 校验方括号内容。保留路径与查询语义。
  两种输入各有回归测试，合法 IPv6、映射 IPv6、zone、userinfo、含 `://[... ]` 的规则路径
  也有用例；修复前新的 Python 边界语料出现 10 条拒绝不一致，修复后全部通过。
- **低：纯异常模拟探针。** 删除 `exports_mosdns.go` 的额外 recover/rethrow/class-conversion
  包装；内部唯一显式 URL 校验错误原本就是 `ValueError`，保留公开 error 返回与共享 catch。
  完整动态值/异常架构留给 S-convert。
- **文档：过期 C23 待定项。** 当前 S-oxidns 已接入完整 native 校验。旧的 1442 条
  normalize 记录全部直接比较通过，没有跳过、pending 或接受差异豁免；原先 25 条 C23
  待定项关闭。同步修正 PROGRESS 中的旧 normalizer API 映射。

## 验证边界

两份独立编写的生成器直接调用 Python 公共函数/真实 hook，不从 Go 推导期望。
HTTP view 仅观察解析结果；真正执行配置空间 middleware、JWT gate、配置 token helper、
repository、response hook。业务 provider handler 属于 C30，本复核不声称重审其转换逻辑。

- `httpx/testdata/review_c05_gen.py`：1427 条，含 1320 次完整请求、30 次全局分类、32 次
  直接解析、5 次 path builder、40 次 URL appending。比较状态码、JSON 字段/值、profile
  响应 header；URL 比较路径/fragment 和有序解码 query pairs。
- `oxidns/testdata/review_c05_gen.py`：1778 条，1293 条预期拒绝；启用所有 template 功能，
  对每个字段与结构节点做类型挑战，另有数值边界、校验先后、未知字段、去重、DNS choice、
  RouterOS/API 凭据与 TLS、动态学习、cron、附件和安全路径。
- 其中 40 条为 20 种 native 文档各自严格/草稿模式：完整/空文档、插件 shape/schema、
  缺引用、缺附件、有效附件、附件内错误、递归 include、不安全 YAML、语法错误。
  保留 inactive template draft；两种模式都校验附件、策略和 rule-DNS。
- 新设置语料比较成功结构、拒绝、中文消息、structured issues、参数调用后状态；不使用旧的
  Unicode/native-library waiver。默认工厂结构一致、深拷贝、未知嵌套字段隔离也单独检查。
- 旧 1442 条 normalize 记录无豁免通过。原 replay/random corpora 作为补充保留。

| 源码分支 | 新边界用例 |
|---|---|
| 显式 ID → path → profile query → profile_id query → header → fallback/default | 各来源不同 ID；空值、重复 query、无效/缺失 ID、fallback、显式空值/NUL、后台/无 endpoint context |
| path 优先使全局 blueprint 成为空间请求；3 个 provider endpoint 例外；全局 endpoint/非 `/api/`/无 endpoint | 全部 12 个 global blueprint、3 个 config 全局 endpoint、普通 API 与非 API path；带 path 的 profiles 路由 |
| 全局请求不缓存 switchable default；普通请求 memo、显式键、fallback 不复用 | 新 direct resolution 序列；保留 C04/scoping 的 metadata-read 计数、commit/transaction/repository 变更、default 切换与新请求隔离测试 |
| 全局 provider 必须 JWT；scoped provider 免 JWT gate，但仍需自身 token/JWT | 两种 provider 各跑无 token、错误/正确 config token、有/无 JWT；带错误空间参数先返回 profile 校验错误 |
| 全局请求不能借 query/header 切空间，scoped 请求不能读默认空间 | 全部请求观察 middleware profile、resolve profile、GetConfig profile_id 和同请求快照身份 |
| template 全部类型/整数/枚举/feature gate、merge 和 first-error | `field/*`、`limit/*`、`inactive/*`、`first-error/*`、`ordered-dedupe/*`、各 feature 的独立约束 |
| rule-DNS 删除/去重/互斥，device policy 委托 | `rule-actions/*`、`dns-choice/*`、`device-policies/*` |
| safe_relative_path、native 严格/草稿、附件错误 | `path/*`、`native/*`、`attachments/*` |

两份 `review_c05.coverage.json` 仅记录静态源码函数/行号；行命中用于补漏，不代替结构比较。

## 接受差异、缓存/日志、结构债

- 按协调者明确决定，不实现 IPvFuture；即使 Python 的 urlsplit 接受，方括号内也必须是
  `netip` 认可的 IPv6。它不是当前客户端可请求的主机。有效 IPv6 与 zone 继续工作。
- 延续 PORTING 现有标准：生成格式、Python 内部错误文本、CPython-only Unicode 边缘
  不追字节。此次没有扩大旧 corpus 的接受差异条件。
- C05 的 resolved memo 用 `{explicit bool, id string}` 结构体键；profile 快照以真实 ID
  为键，没有多字段拼接碰撞。新序列含 NUL 哨兵失败后正常解析，旧 C04 还验证 memo 与
  repository/commit/request 绑定。未发现 C19/C20 式缓存问题。
- scope/normalizer 无日志；配置拉取日志记录固定 service、校验过的 profile、状态、来源和
  有界清理的 UA，不记录 URL query/config token。正常 profile 校验错误由 HTTP handler
  返回，不落入带 source diagnostic 的内部错误日志。未发现此次范围的敏感日志泄露。
- **→ S-convert / S-repo / S-httpx，等 N8 设计**：`mihomo_generate_py.go` 的
  `appendURLQuery`/`fstr`/`raise`/`catch` 与公开动态参数；`repo/defaults.go` 的 jsonx
  factories、`scope.go` compat snapshots、`compat_oxidns.go` 的 `normalizeAction`
  私有动态适配与 `normalizeClientRange` 的 `pyStrip` 边界。地址算法已委托 `clientrange`。
  `httpx/app.go` C05 middleware 自身是 Go context/指针/字符串，其他 query/response 动态
  兼容层属于 S-httpx/C06。未做需要 N8 设计的共享包拆分。

## 命令与输出（检查点）

`.63` 生成新语料（`rt --back` 仅带回合成语料和静态行号）：

```sh
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/httpx/testdata/review_c05.jsonl.gz --back server/internal/httpx/testdata/review_c05.coverage.json -- bash -lc 'PYTHONPATH=$PWD ~/cf-test/.venv/bin/python server/internal/httpx/testdata/review_c05_gen.py'
RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/oxidns/testdata/review_c05.jsonl.gz --back server/internal/oxidns/testdata/review_c05.coverage.json -- bash -lc 'PYTHONPATH=$PWD ~/cf-test/.venv/bin/python server/internal/oxidns/testdata/review_c05_gen.py'
RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go test -count=1 -run TestC05Review ./internal/httpx/ ./internal/oxidns/ ./internal/convert/ && go vet ./...'
```

最后几行：`ok configflow/internal/httpx 1.296s`、`ok configflow/internal/oxidns 2.213s`、
`ok configflow/internal/convert 0.005s`；vet 无输出，exit 0。

初始 `.32` `rt -- server/scripts/proddiff.sh`：exit 0，status/json-leaf/yaml-leaf/text-line
自检 PASS，`new=0 stale=0 coverage_regressions=0`，保留 5 个既有全局差异。
最终全量、生产覆盖说明、mutation 检查及 rebase 状态待补。
