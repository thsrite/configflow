# A12 对抗式复核 — deploy/pushdiff

复核者：独立 Codex agent，非作者；2026-11-17。分支 `go-review-a12`，从
`go-port-a12@02f6a954` 分出。范围为 `backend/agents/push_diff.py` 全部纯函数、
Go `internal/deploy/pushdiff`、共享 `agents/diag` 、`yamlx` 与 `jsonresp` 的相关路径。
按 AGENT-BRIEF、PORTING 顶部、a14-publish 设计 A12、A11/A12 验收和 A11/R06 问题类别逐项核对。
当前树没有 `review-r07.md`（R07 仍 missing），已核对 WORKFLOW 尾部收录的 R07 共享路径回归规则。
无生产访问、无设备写入、无 push/PR、无子 agent，输入全部合成。

方法：逐分支核对 Python，新增独立 `testdata/gen_review_a12.py` 在测试服务器调用真实 Python。
`testdata/review.json.gz` 包含 77 行：逐键/节点类型与深层输入、所有凭据键和后缀（含大写/下划线）、
多行值、URL/userinfo/opaque，以及 713 条交错 CJK/凭据规则的 180 个分页/搜索结果。
正常 JSON（排除 raw 原生排版）严格比较；Python 自身丢失键类型的一个用例单独明确断言修正结果。
原有 32 测试/67 行语料继续原样通过。raw 另验补丁可还原文档、解析后语义一致、隐藏视图不含凭据。

## 发现

实现问题均已修复；最终门禁进行中，暂不设置 verified。

### 中

- **F1 — 共享诊断 URL 正则不适用于解码后的配置字符串，反斜杠后的凭据漏标。**
  FailureText 的 URL 识别故意在 `\` 前结束（避免吞并转义引号），Python push_diff 的模式允许该字符。
  `https://rules.example.com/?token=synthetic\tail` 只标记 `synthetic`；
  `https://rules.example.com/prefix\part?api_key=synthetic-tail` 完全漏标；
  userinfo 中带反斜杠时只隐藏截断的 URL，密码尾部仍在隐藏视图及搜索文本中。
  修复：共享 diag 中复用同一个 URL 前缀及字符排除表，区分诊断文本和配置字面值两个模式，
  `CredentialSpans` 使用后者；非法 userinfo 继续由 net/url 整段隐藏。没有在 pushdiff 私抄正则。
  测试：`TestReviewPythonBoundary/spans`、`TestReviewCredentialViewsAndSearch`，覆盖值、JSON 显示键、
  row.key/path、raw、节点名称分页搜索；普通节点 server:port 仍能搜索。

- **F2 — provider 规范化丢失锚点，使整份 raw diff 的凭据标记失效。**
  `agentPaths` 重建 provider 映射但保留外部 alias；`proxy-providers: &providers …; copy: *providers`
  因而输出不存在定义的 `*providers`。即便路径原本已规范，仍会触发。
  `rawLines` 解析失败时直接返回未标记文本，连不相关的 `password` 和 URL token 都暴露在隐藏视图中。
  修复：导出并扩展 yamlx 原有的别名迁移函数 `MaterializeAliases`，在 canonical 输出前使用；
  仅迁移首次引用，保留共享链，复制的定义使用独立锚点名，provider 之外的别名保留原值。
  解析失败时 raw 整段标为敏感，reveal 仍保留全部原文。
  测试：`TestReviewProviderAliasesKeepRawValidAndSecret`（两种 provider × 已规范/未规范路径），
  `TestReviewRawMarkingFailsClosed`、yamlx `TestMaterializeAliasesRenamesCopiedDefinitions`。

- **F3 — 将深度 64 的显示截断用于身份比较，漏掉真实节点及列表变更。**
  `valueID` 直接调用 `show`，80 层输入的不同叶子均成为 `"…"`：节点 modified=0，
  列表 added/removed=0 且没有变更条目。JSON 显示还会混淆嵌套整数键和字符串键。
  修复：共享 yamlx 新增 `ValueHash`，按 Equal 的有效值、类型、映射无序和数值规则计算身份，
  对 alias 图记忆化，无显示深度截断、不扩展用户值；节点、标量列表和映射键共用。
  测试：真实 Python `deep-node`、`deep-list`；`typed-list`、`typed-node` 确认类型变更保留；
  `TestValueHashUsesEffectiveTypedValues` 覆盖排序、merge、alias、键类型和数值拼写。
  Python 的节点 `_json` 自身会把 `{1: value}` 与 `{'1': value}` 混为一谈：Go 明确修复，报告
  modified=1、两个字段变更，原 Python 为 0；这是有说明的 Python bug 修复。

- **F5 — JSON 转义放大了预算，改变首页及压缩分页结果。**
  40 个修改节点、每个 20 个长字段，`&`/`<` 被嵌套 MarshalJSON 重新 HTML 转义后，
  Python 一页 40 个、Go 只有 8 个；U+2028/U+2029 则是 Python 17 个、Go 8 个，
  首页条数也随之变化。这不是 raw 排版差异。修复：所有显示 union 和 JSON DTO 复用共享
  jsonresp 编码；共享 `EncodeUTF8` 保持合法 JSON 的字面 Unicode，逐个跳过转义反斜杠，
  不改写用户的字面 `\\u2028`。压缩记录的真实 UTF-8 字节重新与预算一致，不放宽任何预算。
  测试：真实 Python `node-budget/{html,cjk,js-separators}` 逐项比较首页、available、页长及
  next_offset；`jsonresp.TestEncodeUTF8KeepsLiteralEscapesAndValues` 验证 JSON 回读及字面转义不变。

### 低

- **F4 — 数字按拼写作为身份，制造不存在的节点、列表及键变更。**
  `1.00` → `1.0` 的节点被计为 modified=1 却没有字段变化；含其他编辑的列表额外增加/删除该数值；
  映射 `1.00: before` → `1.0: after` 被报成两个键而不是一个修改。
  修复与 F3 共用 `yamlx.ValueHash`，复用已有数字规范化函数，未复制 Python/私有数值解析器。
  测试：真实 Python `numeric-node`、`numeric-list`、`numeric-key`；hash 单测同时保留 int/float 区别。

## 其余核对结果与接受差异

- 共享 `diag.IsCredentialKey`/`CredentialSpans` 与 redact stored finder 继续是唯一来源；
  FailureText 的 `SecretSpans`、UUID/quoted 多行模式已由 A11 使用，本模块对 YAML 解析后的完整标量标记，
  不需要复制这些日志正则。所有 secret 键、后缀、大小写/下划线、Bearer 跨行值均与 Python 对照。
  hosts/policy/provider 等 data-keyed 容器继续按实际路径排除敏感字段判断，键中的 URL 仍独立标记。
- raw 的 yaml.v3 排版、patience hunk 选择和格式提示阈值差异可保留；不能接受语义丢失或未标记凭据，
  F2/F3 已修正。`FuzzConfig` 新增 raw 编码→Parse→Equal 不变量：十秒预跑发现浮点规范化会舍入原始
  数字文本，现 canonical 保留原浮点拼写，最小输入保存在 `fuzz/FuzzConfig/84f02c89439aa892`。
  这项保留更强的原始值保证，不引入 Python 数字模拟。
- 三个 YAML 入口均使用 yamlx.Parse：D1、深度 40 指数 alias 拒绝、线性 alias 链接受、自引用/多文档拒绝；
  无 yamlx.Clone。数值比较复用 facade 原生有效值规则；不按 `False`/`0x0` 的标签文本猜值。
- 500 文件、1500 raw 行/160 KiB、60000 输入行、240 字符值、100000 retained entries、
  16/48 MiB 保留预算和 512 KiB 分页上限均继续受原单测保护；CJK 按字符截断、预算按 UTF-8 字节。
  180 个独立 Python 分页结果覆盖首页/20/50/200/末页、跨页搜索、凭据不参与搜索。
  stored finder 的排除、局部替换、Unicode 范围合并及并发不可变读取保持通过。
- Build 的摘要优先级、缺失/空基线、Receipt.ComparisonConfig、只读变化文件、重复节点配对、规则多重集
  逐分支无额外未决差异。三个纯函数边界不增 HTTP 路由；A13 缓存/路由测试继续属于 A13。
- 无 Python 模拟、无新增依赖；生产参数未被测试 seam 替换。共享 yamlx/jsonresp 改动需在合并树跑全量与 difftest。
- N7-3a 的共享 API 已核对：AllPairs 采用 go 的唯一实现；ProjectJSONKeys 是可变的 JSON 边界投影，
  会合并整数/字符串键转换后的冲突，不能用于 YAML 身份比较，也不能替代逐片凭据标记。
  A12 保留节点类型，直接复用 AllPairs/Get/ToJSON/Equal，无额外 YAML merge/key 投影副本。

## 命令与结果

最终门禁固定 `sunbroke@192.168.0.64`，`GOMAXPROCS=4`；初始 oracle 在 .64、预算补充在 .32。以下命令可重新生成独立 oracle；修复前失败证据紧随其后：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/deploy/pushdiff/testdata/review.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/pushdiff/testdata/gen_review_a12.py && cd server && go test -count=1 -run TestReviewPythonBoundary -v ./internal/deploy/pushdiff'
```

```text
config/deep-list: entries length got 0 want 2
config/numeric-list: entries length got 4 want 2
config/numeric-key: total got 2 want 1
nodes/deep-node: modified got 0 want 1
nodes/numeric-node: modified got 1 want 0
spans/...token=synthetic\tail: got synthetic want synthetic\tail
spans/...prefix\part?api_key=synthetic-tail: length got 0 want 1
node-budget/html: page_count got 8 want 40
node-budget/js-separators: next_offset got 8 want 17
TestReviewProviderAliasesKeepRawValidAndSecret: unknown anchor 'providers' referenced; raw secrets exposed (4 cases)
```

修复后迭代：`go test -count=1 ./internal/deploy/pushdiff ./internal/yamlx` 通过；
`FuzzConfig` 新不变量预跑 10 秒通过（41409 execs）。补充预算 oracle 在 .32 运行（77 行），pushdiff/jsonresp/yamlx 全部通过。完整 .64 门禁通过；最新 O3 合并树的 .32 集成补验进行中。


最终完整门禁命令（worktree 根目录，.64，全部 exit 0）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -o pipefail; bash server/internal/deploy/pushdiff/testdata/gate.sh 2>&1 | tee /tmp/configflow-a12-review-final.log'
```

`gate.sh` 展开为：

```sh
cd server
test -z "$(gofmt -l internal/ tools/)"
go vet ./...
go test -race -p=4 -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx ./internal/jsonresp
go test -p=4 -count=1 ./...
for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done
for target in FuzzConfig FuzzRuleEntries FuzzNodeItems FuzzCredentialSpans; do
  go test -run '^$' -fuzz "^${target}$" -fuzztime=60s ./internal/deploy/pushdiff
done
cd ..
server/scripts/proddiff.sh
```

补充的 .32 交叉检查：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/deploy/pushdiff/testdata/review.json.gz --back server/internal/deploy/pushdiff/review_test.go -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/pushdiff/testdata/gen_review_a12.py && cd server && gofmt -w internal/deploy/pushdiff/review_test.go && go test -count=1 ./internal/deploy/pushdiff ./internal/jsonresp ./internal/yamlx'
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/)" && go vet ./... && go test -count=1 ./internal/installer'
```

```text
A12 independent Python review: 77 rows
ok configflow/internal/deploy/pushdiff 4.005s
ok configflow/internal/jsonresp 0.002s
ok configflow/internal/yamlx 1.858s
ok configflow/internal/installer 156.155s
```

新增文件（复核部分）：review_test.go 182 行、gen_review_a12.py 94 行、native_hash.go 83 行、
native_hash_test.go 65 行、utf8_test.go 23 行、最小 fuzz 输入 3 行、review.json.gz 37126 bytes；
以及本复核记录。没有调试文件或临时 testdata。


`.64` 完整门禁结果（基线 `ac1f2377` + A12 独立复核修复，最后输出摘要）：

```text
gofmt: no output; go vet: exit 0
race: pushdiff 40.769s; yamlx 12.667s; jsonresp 1.008s; diag [no test files]
full go test -p=4 -count=1 ./...: exit 0
ok configflow/internal/weburl 0.004s
ok configflow/internal/yamlx 1.307s
ok configflow/internal/yamlx/yamlxtest 0.002s
ok configflow/tools/proddiff/generator 0.009s
CONFIGFLOW_DIFF all 12: agents 140.818s, aggregations 38.756s, subscriptions 94.471s,
 devicenotes 0.641s, discovery 2.518s, health 13.910s, httpx 1.196s, mosdns 1.775s,
 refresh 3.658s, subs 2.023s, substore 6.213s, webdav 19.005s — all ok
FuzzConfig          60s 136157 execs PASS (61.074s)
FuzzRuleEntries     60s 122435 execs PASS (61.030s)
FuzzNodeItems       60s 245125 execs PASS (61.029s)
FuzzCredentialSpans 60s 155671 execs PASS (61.028s)
proddiff: self_check=PASS coverage_regressions=0 unmapped_routes=0
agent-deployment-bundle: compared=3 success=3 (C31 已独立生成，无 partial)
baseline: new=0 stale=0
```

结束前再次 `git rebase go` 纳入 O3 `197c3c3f`。A12/yamlx/jsonresp 复核实现不变，
仅新增集成分支的生命周期/HTTP shutdown 变更；四个纯函数 fuzz 不重复。
在最新合并树补验以下命令（.32；结果待补）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -euo pipefail; cd server; test -z "$(gofmt -l internal/ tools/)"; go vet ./...; go test -race -p=4 -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx ./internal/jsonresp; go test -p=4 -count=1 ./...; for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done; cd ..; server/scripts/proddiff.sh'
```
