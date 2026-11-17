# N7-3b：YAML reader 迁移与边界复核

分支：`go-n7-3b`。完整源码验收点：`03f521b5`，基于 `go@6b50151c`；
N7-3a 集成补验基点：`go@8303397c`，补验源码：`c4392d47`；
最终补验基点：`go@197c3c3f`，补验源码：`5acf98f7`；
交回前最后 rebase 到 `go@aaf5f0dc`，无冲突。
测试固定 `RT_HOST=sunbroke@192.168.0.32`；未在 Mac 跑测试、构建或检查。
未派子 agent，未推送，未改生产差分基线。最终输出见下文。

## 迁移调用点

| 调用点 | 最终实现 |
|---|---|
| rulefetch.normalizeMihomoYAML | Parse/Node；共享 Edit 先 splice，必要时 D3 重编码 |
| rulefetch.domainEntries / InspectDomainContent | 直接读取 payload 标量 Node；共享 Edit 修改 |
| rulefetch.remoteTextEntries / ValidateRemoteRuleContent | Parse；按物理 Node 报 anchor/alias 位置 |
| rulefetch.ReadRulePayload | Parse；只读取 payload，不把整个文档转成 any/JSON |
| rulefetch.AppendRulePayload/AppendRuleLines/RemoveRulePayload | Node sequence 编辑；失败返回 typed validation error |
| rulefetch.pyerrors | 删除纯递归异常模拟探针 |
| refresh 的两处测试读取 | Parse/类型化 Unmarshal，移除旧 SafeLoad |
| refresh.SourceCacheMetadata/RememberSourceDownload | 元数据读取和写缓存前执行共享策略门 |
| refresh.DownloadSubscriptionNodes/FetchSubscriptionSource | 新下载和历史下载缓存复用均经过策略门 |
| convert.loadProxyYAML / 两个 structured-proxy API | Parse + ProjectJSONKeys；所有 Node 只转换一次 |
| api/subscriptions.yamlContent | 程序生成的 JSON 通过 FromJSON + Marshal；不再读成 YAML |
| api/subscriptions.cachedProxies | lenient 的新 YAML 错误按序号/类别警告并累计 skipped |
| rulesetfmt.HasYAMLPayload | 修正前置注释/空行/--- 后的 flow-root 探测；不写 scanner |

删除 `convert/proxyyaml_loose.go` 的私有 resolver/constructor/merge 展开和递归
placeholder。键转换统一使用共享 `ProjectJSONKeys(StringifyScalarKeys)`；转换后
重名沿用最后值、首次位置的既有 JSON 边界语义。复杂/不支持的键明确拒绝。
缓存占用估算改为 Go 值，不再模拟 CPython 分配；它只影响缓存准入，不拒绝输入。

共享提交全部接入：`0b98b42e`、`dc3f339c`、`23196ddd`、`a7acc788`、
`a5f553d0`、`4ad822f5`、`942e752f`、`09740db3`（最后一个与 `ae109f82`
逻辑相同，只是 gofmt）。最终 rebase 时 8 个等价共享提交全部自动跳过，
`internal/yamlx` 相对 go 无差异。没有私写标签、merge 或键投影规则。
旧加载器和 internal/pyyaml 按分工保留，最后由 N7-3d 清理。

## 发现的问题与修复

- **高：代理私有加载路径绕过统一策略，并可能制造递归占位值。** 改成共享 Parse；
  D1、标签/merge 校验、递归/放大预算统一执行，不产生 null 替代。
- **高：策略错误被当作 URI/base64 继续请求 Sub-Store。** 两个内部公开读取 API
  返回 error；只有真正 ErrSyntax/非结构化输入保持原来的尝试其他解析器分支。
  策略/alias 回归断言 Sub-Store 调用为零、代理缓存为零。
- **高：下载文本在安全校验前可能替换缓存。** 新下载、元数据和历史缓存复用
  先校验；失败不替换 transport cache 或 validated-node cache，也不改原缓存字节。
- **中：Append 仅返回 string，Edit 失败会被吞掉。** 三个 Append API 和
  ConnectionDeps 回调改为 `(string,error)`；rules/connection 和 discovery/write
  最小适配，错误时不宣称添加成功。Remove 的编辑错误也进入 typed 400 通道。
- **中：旧 Python/Go 编辑器的两个 bug。** 清空无缩进 sequence 曾写出无效
  `payload:\n[]`；flow 列表含 alias 时追加可能插到旧条目前。现在输出合法空列表，
  新项始终位于旧项之后。
- **中：Python 与旧 Go 同源的格式 hint bug。** `# header`/空行/单独 `---`
  后接 flow payload 曾被当纯文本，可能追加出无效 YAML 或生成空 DNS 规则集。
  现在正确识别；nil payload 的追加重新 Read 校验，普通 Classical 文本仍保持不透明。
  未修改 rules/matcher 的裸 decoder。
- **迁移内存风险：标量 alias 重复序列化。** 容器和标量均 memo；1MiB 字符串被
  100 个 alias 引用时共用 backing data，避免按出现次数复制大值。

## 每个错误调用点的选择

生成器、convert/repo rawDialer、repo 导入改写和 provider delivery 使用现有 error
通道。ProxyYAMLError 保留 yamlx cause，并可识别为策略校验错误；repo 适配为
ValueError/400。health 对失败节点返回既有 `error_code=config`，不会错误地算作
需要远端转换或测量成功。

cachedProxies 的 strict 模式返回 typed 400，不同时记录错误；lenient 模式跳过
该节点，只记录 `node_index` 和 `error_kind`，本地累计后记录 `skipped=N`。
HTTP 字段不变，其他节点照常、按原顺序返回。回归包含带合成密码的 alias/policy
错误，断言日志中没有节点名、节点内容、密码或 token。

## 迁移门与分类数量

旧 Python 语料/整套测试录制作为旧边界 oracle，逐行运行新调用方。所有结果字段、
状态/校验通道和规则值保持严格比较；只有生成/编辑后的 YAML 文本改为 Node 结构比较。
另有独立的精确 splice/no-op/CRLF/BOM/引号/注释边界断言。策略拒绝必须保留原文或
返回同一校验类别和精确共享诊断。特殊结果按原 suite/function/input 的指纹固定
完整新输出/错误指纹，不按函数整体放行。没有新增 skip；原有 URL/录制键标记差异
沿用原测试的登记规则。

rulefetch：三个语料共 190415 行，本轮改变 **9079 行**，全部分类：

| 类别 | corpus | whole-suite | C14 review |
|---|---:|---:|---:|
| D1 重复键拒绝 | 63 | 20 | 17 |
| §5.4 标签/merge/文档校验 | 21 | 0 | 18 |
| 原生诊断/位置 | 6417 | 32 | 11 |
| D3 排版、Node 语义相等 | 2327 | 68 | 36 |
| D3 跨行引号折叠 | 9 | 2 | 0 |
| alias 拒绝的诊断汇总 | 4 | 2 | 0 |
| 原生 Node 映射键 | 0 | 0 | 5 |
| binary 标量文本 | 0 | 0 | 1 |
| 修正清空 sequence 的 Python bug | 25 | 0 | 0 |
| 修正 alias 追加位置的 Python bug | 1 | 0 | 0 |

其中特殊边界 49 次调用逐条复核，固定为 44 个不同输入的精确预期。
第五个共享补丁使 21 行原先被 oracle 省略的英文诊断变成可见中文；这些行额外
断言错误的其余元数据完全一致，且诊断恰为原生 cause。

convert：48 个不同改变输入：原生标量 schema 28、D1 4、§5.4 7、递归拒绝 5、
原生 JSON 数字写法 2、成员顺序 2。缓存重复读取使用同一精确预期。
repo 调用方 golden：30 个显式原生预期，分别为 schema 18、D1 3、§5.4 6、
JSON 键边界拒绝 2、成员顺序 1。
C09 消费者 1536 行：198 行排版结构比较、36 行保留中文上下文并附精确原生诊断；
其他结果字段仍严格比较。

## alias、重编码和接受的差异

各读取/检查/规范化/编辑/远端校验入口覆盖共享 diamond40、线性 alias40、自引用。
要求在预算内拒绝且保留原文，或保持共享；没有指数展开、栈溢出或静默 null。
代理标量还覆盖 1MiB×100 的 backing-data 共享。运行时 yamlx.Clone 调用为零。

跨行引号按 YAML 折叠；含空格的非法域名仍被规则校验拒绝，合法 escaped continuation
与一行域名的匹配结果完全一致。重编码回归保留 timestamp、大整数、binary、custom
标签、.inf/.nan、像数字的字符串，以及原有规则顺序和头部注释。
splice 路径逐字节保留未改动部分；只读/无实际改动保持原文。需要重编码时保留
语义和注释，存储文本只在实际编辑时发生一次格式规范化。delivery-only 的排版
属于 §5.7 生成字节差异，不写回源内容/缓存，也不改变其指纹。
完整差异说明和三个 Python bug 见 behaviour-changes.md。

## 验收命令和最后输出

所有命令固定 `.32`，从仓库根目录运行。完整日志在测试机 `/tmp/n73b-complete-*.log`。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/ tools/'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go vet ./...'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p=4 -count=1 -timeout=30m ./internal/rulefetch ./internal/refresh ./internal/convert ./internal/repo ./internal/provider ./internal/health ./internal/rules ./internal/api/subscriptions ./internal/discovery ./internal/rulesetfmt ./internal/deploy ./internal/yamlx'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -p=4 -count=1 ./...'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && for d in $(go list ./... | rg difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d" || exit; done'
RT_HOST=sunbroke@192.168.0.32 rt -- server/scripts/proddiff.sh
```

Fuzz 每个目标使用相同命令模板，实际由远端 Python 调度、最多两个独立任务并行：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^<目标>$" -fuzztime=60s -parallel=2 ./internal/<包>'
```

目标：rulefetch/FuzzRuleYAMLBoundary、refresh/FuzzSourceYAMLPolicy、
convert/FuzzStructuredProxyYAML；deploy/FuzzConvertMosDNSRules、FuzzSourceExcerpt、
FuzzOpenArchive；yamlx/FuzzNativeRoundTrip、FuzzNativeNoPanic、FuzzEditScalar、
FuzzEditDeletePair。共 10 项，各 60 秒。

本基点最终输出（exit 0）：

```text
gofmt: 无输出；go vet: 无输出
race: rulefetch39.323s refresh306.831s convert101.891s repo123.765s
race: provider211.211s health257.519s rules12.785s subscriptions3.345s
race: discovery9.970s rulesetfmt1.017s deploy28.660s yamlx16.699s
ok configflow/internal/yamlx 4.468s
?  configflow/tools/proddiff/generator [no test files]
```

全部10个 difftest 包均通过：api/agents120.217s、api/subscriptions77.972s、
discovery2.366s、health13.810s、httpx1.420s、mosdns1.798s、refresh4.423s、
subs3.050s、substore7.595s、webdav24.182s。

| Fuzz 目标 | executions | 结果/耗时 |
|---|---:|---|
| FuzzRuleYAMLBoundary | 21139 | PASS61.061s |
| FuzzSourceYAMLPolicy | 37924 | PASS61.034s |
| FuzzStructuredProxyYAML | 26857 | PASS61.029s |
| FuzzConvertMosDNSRules | 82132 | PASS61.191s |
| FuzzSourceExcerpt | 51113 | PASS61.083s |
| FuzzOpenArchive | 343607 | PASS61.049s |
| FuzzEditScalar | 50980 | PASS61.105s |
| FuzzEditDeletePair | 49183 | PASS61.089s |
| FuzzNativeRoundTrip | 119231 | PASS61.054s |
| FuzzNativeNoPanic | 20959 | PASS61.026s |

```text
fuzz_targets=10 failed=False
proddiff: cases=2831 compared=415 self_check=PASS new=0 stale=0
baseline: new=0 stale=0
```

生产快照没有触发新增差异，未调整基线。

## N7-3a 合入后的集成补验

已 rebase 到 `go@8303397c`（包括 C31 与 N7-3a）。8 个重复共享提交全部自动
跳过，`internal/yamlx` 与 go 完全一致。只有文档追加冲突，保留两方内容；
没有运行时代码冲突。补验源码为 `c4392d47`，日志在 `.32` 的
`/tmp/n73b-after-a-*.log`。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '
set -euo pipefail
cd server
gofmt -l internal/ tools/ > /tmp/n73b-after-a-format.log
test ! -s /tmp/n73b-after-a-format.log
go vet ./... > /tmp/n73b-after-a-vet.log 2>&1
go test -race -count=1 -timeout=30m ./internal/refresh > /tmp/n73b-after-a-refresh.log 2>&1 || { tail -n45 /tmp/n73b-after-a-refresh.log; exit 1; }
tail -n3 /tmp/n73b-after-a-refresh.log
for d in $(go list ./... | rg difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done > /tmp/n73b-after-a-difftests.log 2>&1 || { tail -n70 /tmp/n73b-after-a-difftests.log; exit 1; }
tail -n16 /tmp/n73b-after-a-difftests.log
cd ..
server/scripts/proddiff.sh > /tmp/n73b-after-a-proddiff.log 2>&1 || { tail -n25 /tmp/n73b-after-a-proddiff.log; exit 1; }
tail -n1 /tmp/n73b-after-a-proddiff.log
'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server && go test -p=4 -count=1 ./... > /tmp/n73b-after-a-full.log 2>&1; result=$?; tail -n18 /tmp/n73b-after-a-full.log; exit "$result"'
```

另外逐行核对差分日志：恰好 12 条 `ok`，没有 FAIL/build failed/panic。
最后的全量测试用 `--no-sync` 排队，避免同步干扰正在运行的命令。
此前一轮 C31 测试与同步重叠，短暂读到不完整测试文件；该轮失败不计验收，
由完整源码上的本次全量复验替代。

```text
gofmt: 无输出；go vet: 无输出
ok configflow/internal/refresh 278.151s
ok configflow/internal/api/agents/difftest 126.474s
ok configflow/internal/api/aggregations/difftest 27.400s
ok configflow/internal/api/subscriptions/difftest 78.602s
ok configflow/internal/devicenotes/difftest 0.591s
ok configflow/internal/discovery/difftest 2.191s
ok configflow/internal/health/difftest 13.818s
ok configflow/internal/httpx/difftest 1.391s
ok configflow/internal/mosdns/difftest 1.754s
ok configflow/internal/refresh/difftest 3.423s
ok configflow/internal/subs/difftest 1.686s
ok configflow/internal/substore/difftest 5.930s
ok configflow/internal/webdav/difftest 18.006s
proddiff: cases=2831 compared=435 self_check=PASS new=0 stale=0
baseline: new=0 stale=0
```

当前基点全量 `go test -p=4 -count=1 ./...` 也已完成，exit 0，末行：

```text
ok configflow/internal/subs 8.019s
ok configflow/internal/substore 2.734s
ok configflow/internal/yamlx 1.948s
ok configflow/internal/yamlx/yamlxtest 0.002s
ok configflow/tools/proddiff/generator 0.010s
```

原先的集成待办已关闭：refresh 委托的 hosts、dialer、proxy-list 读取已经在
a 合入后复验，策略/alias 拒绝不写缓存的边界也通过。当前没有未关闭的本分支
功能问题。其余裸 decoder 与旧加载器删除仍按既定分工由 N7-3d 完成。

## O3 合入后的最终补验

`go@197c3c3f` 新增 installer 测试及 O3 生命周期修复后再次无冲突 rebase。
考虑 Detach/请求绑定影响刷新入口，在 `.32` 补跑以下命令；两组独立检查在
同一个前台 shell 中并行，分别等待并核对退出码，结果均为 exit 0。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail
cd server
gofmt -l internal/ tools/ > /tmp/n73b-final-format.log
test ! -s /tmp/n73b-final-format.log
go vet ./... > /tmp/n73b-final-vet.log 2>&1
(go test -race -count=1 -timeout=30m ./internal/refresh > /tmp/n73b-final-refresh.log 2>&1) &
refresh_pid=$!
(for d in $(go list ./... | rg difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d" || exit; done > /tmp/n73b-final-difftests.log 2>&1
cd ..
server/scripts/proddiff.sh > /tmp/n73b-final-proddiff.log 2>&1) &
diff_pid=$!
result=0
wait "$refresh_pid" || result=1
wait "$diff_pid" || result=1
tail -n5 /tmp/n73b-final-refresh.log
tail -n16 /tmp/n73b-final-difftests.log
test ! -f /tmp/n73b-final-proddiff.log || tail -n1 /tmp/n73b-final-proddiff.log
exit "$result"'
```

```text
gofmt: 无输出；go vet: 无输出
ok configflow/internal/refresh 393.973s
ok configflow/internal/api/agents/difftest 161.325s
ok configflow/internal/api/aggregations/difftest 51.464s
ok configflow/internal/api/subscriptions/difftest 154.841s
ok configflow/internal/devicenotes/difftest 1.157s
ok configflow/internal/discovery/difftest 5.101s
ok configflow/internal/health/difftest 15.447s
ok configflow/internal/httpx/difftest 2.904s
ok configflow/internal/mosdns/difftest 3.506s
ok configflow/internal/refresh/difftest 6.246s
ok configflow/internal/subs/difftest 3.119s
ok configflow/internal/substore/difftest 10.482s
ok configflow/internal/webdav/difftest 29.358s
proddiff: cases=2831 compared=435 self_check=PASS new=0 stale=0
baseline: new=0 stale=0
```

验收结束后的收尾 rebase 额外带入 C31 复核提交 `aaf5f0dc`（aggregations/
devicenotes 及其测试/文档），无冲突，未改变 b 或共享 yamlx 的实现。以上最终
补验结果对应 `197c3c3f`，不冒充已覆盖这个随后新增的 C31 提交；交由协调者
合并前在集成分支复验。没有推送。
