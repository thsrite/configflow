# batch-49 复核：O17 1.7 OxiDNS 类型化第一轮

- 分支 `batch-49`（原 `go-o17-typed-oxidns@1a58914e8`），已 rebase 到 `go@1e43a4766`。
- 复核人：Opus 5.5（HR），2026-11-23。

## rebase

- `settings.go` 一处冲突：go 侧把 `splitLines` 移到 `textx.SplitLines`、`requestTypeError` 改名 `checkRequestTypes`；
  本分支把 `checkTemplate` 移到 `settings_template.go`。按 go 为准解决：删掉 `settings.go` 里的旧 `checkTemplate`/`splitLines`，
  `settings_template.go` 的 `checkMatches` 改用 `textx.SplitLines`。其余 7 个提交无冲突。

## 重点核对

### 1. 生成的 OxiDNS 配置逐字节不变

- pyref 提交只动 metadata：逐个解压 13 个黄金文件（oxidns 8、mosdns 5），比较 `go` 与分支的 JSON，
  只有 `metadata` 不同，`entries`/`version` 完全相同。
- `templateDocument` 改读 `templateSettings`：逐字段对照旧 `get(s, …)`，取值来源与默认值一致；
  `upstreamChain` 的 `lazy_cache_ttl` 从 big.Int 重新生成十进制整数节点（见低-1）。
- 验证见文末（pyref check、boundary 参考、mosdns golden、proddiff）。

### 2. 存储对象保持开放

逐条找「类型化解码后写回存储」的路径：

- `decodeTemplate` 只用于校验和生成；唯一写回是去重后的四个选择列表（与旧 `set(s, key, unique(...))` 相同，
  只在模板模式、`NormalizeSettings` 内）。设置对象其余成员仍是原 YAML 树，未知成员和顺序不动。
- `RuleAction.node()` 只在 `NormalizeAction` 产出规范形式时用，和旧代码一样只重建 `action/source/upstream`，
  `upstream` 仍是原节点的 clone（未知成员保留）。
- 设备策略行：`devicenotes.ParsePolicies` 本来就拒绝 `ip/name/mode` 以外的成员，旧代码也写回规范化行，
  `policyRowsNode` 输出与旧 `NormalizePolicies` 相同。
- 设备备注：`DeviceNotesJSON` 的两个调用方（`api/agents/client_tools_adapter.go`）都在 `SetNote`/`SavePolicyRows`
  之后调用，此时 `p.notes` 来自 `ToJSON(notesNode)`，与旧路径同一字节来源。
- `ReadSettingsHead` 只读不写；`removeRule` 仍从 `OxiDNS.Node()` 读出完整树、编辑、写回（未改）。
- `knownRules` 返回指向 `source.Profile.RuleConfigs` 的指针，已确认包内没有通过它改字段的地方。

结论：没有新增的丢未知成员或改顺序的写回路径；`Revision`/ETag 仍是存储对象规范化哈希，未改。

### 3. 宽松读取

- `templateSettings`（`jsondecode.Member`）：大小写敏感、缺失/null/类型不对时 `OK=false`，与旧 `isStr/isBool/intOf`
  判断一致；整数用 int64 解码，越界或非整数同样失败，文案沿用 `%s 必须为 %d–%d 的整数`。
  `custom_matches[].enabled` 非布尔时旧/新都不跳过（且 `checkRequestTypes` 先拒绝）。
  `api.ssl`、`ros_*.args.tls`、`learn_args` 的「必须为对象」判断：Member[struct] 只对 JSON 对象 OK，与 `isMap` 一致；
  null 也报错（`Present && !OK`），与旧 `has && !isMap` 一致。
- `RuleAction`：`action`/`source` 类型不对读成 `""`，不在允许列表里，仍报原错误；没有放行分支。
- `ReadSettingsHead`：设置不是对象时三项为空、`FollowOK=false`，调用方（`syncFollow`）与旧 `SettingsStrings` 的
  `ok=false` 同样直接返回；`config_mode` 不是文本时读成 `""`，不会被当成 native（与旧 `str()` 同）。
- 规则/组/Agent：旧 `Profile.doc` 本来就是从 `model.RuleConfig`/`ProxyGroup`/`Agent` 序列化出来的，
  所以 `messageNullRuleType`/`messageNullRulesetType` 两个分支在旧代码里已不可达，删除不改变行为。
  `dnsBindings` 的 `upstream_tag != ""`、`ProfileAgents` 的默认 profile 等都与旧 Node 路径等价（逐项对过 omitzero）。

### 4. PolicyView JSON 形状

`Policies` 仍是 `*Node`（`policyRowsNode`，成员 `ip, name, mode`），名字覆盖写到 typed 行后再转节点，
输出与旧 `set(row, "name", …)` 相同（旧行本来就有 `name` 成员，覆盖不改顺序）。routers 未改。

## 发现

- 高：无。
- 中-1（已修，93a95b533）：rebase 到 go 后 `PYREF_MODE=check` 失败——go 上 e6927a915 把行拆分移到
  `internal/textx`、ff78a7e51 删了 `convert/ipaddr.go`，`tools/pyref/sources.json` 没跟上：oxidns 14 条参考缺
  `textx/textx.go`（undeclared sources），mosdns 5 条仍列已删的 `ipaddr.go`（stat 失败）。go 本身也有这个问题
  （普通 `go test` 不查，只有 check/record 模式会报）。修法：sources.json 增删这两个文件，
  `PYREF_MODE=record PYREF_ACCEPT=1` 重录 oxidns/mosdns，12 个黄金文件逐个对比 go 与分支，只有 `metadata` 变。
  另：`-suggest-sources` 还提示 routersplit 3 条参考缺 `logx/*.go`，不在本批范围，留给协调者。
- 低-1：原生文档里 `lazy_cache_ttl` 写成非十进制整数（如 `0x10`、`1_000`）时，固定上游链 `cache` 插件里
  改为输出十进制（旧代码 clone 原节点）。OxiDNS 读到的数值相同；只影响畸形/少见写法的输出字节。不修。
- 低-2：`takeOver` 的提示里，规则 id 不在当前规则列表时（`rule_actions` 与选择列表里残留同一个已删规则），
  规则名由 `<nil>`（旧 Go 的 `displayValue(nil)`）变成空串。只影响一条提示文字，新行为更合理。不修。
- 低-3：`ReadSettingsHead` 不再经过 `OxiDNS.Node()`，存储 JSON 有重复键时（Go 写入不会产生）不再报错而取最后一个。不修。

## 验证（全部在 .32，HEAD=93a95b533 内容）

- `gofmt -l` 无输出；`go vet ./...` 通过。
- `-race -p 6`：oxidns 及调用方（agents/dnsfollow、agents/dnsha、agents/rulesync，api/agents、clientdevices、config、generate、
  oxidns、proxygroups、routers、rulelibrary、rules，clients、configgen、deploy、devicenotes、gateway/onboarding、ippresence、
  routers、setup、tools/proddiff）全部 ok。
- difftest（`CONFIGFLOW_DIFF=1 -tags configflow_difftest`）：oxidns、mosdns(+difftest)、api/oxidns、api/rules、api/rulelibrary、
  api/proxygroups、api/config、api/clientdevices、api/agents、api/generate、devicenotes、agents/dnsfollow、agents/dnsha、
  agents/rulesync、configgen、gateway/onboarding 全部 ok。
- `PYREF_MODE=check go test ./internal/oxidns ./internal/mosdns`：修复后 ok（oxidns 98.979s、mosdns 63.566s）；`tools/pyref` ok。
- fuzz 60 s：FuzzZoneValidation PASS（72.1 万次）、FuzzNativeYAMLRoundTrip PASS（50.0 万次）。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`：baseline new=0 stale=0，self_check PASS，GATE STAGE PASS。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-49 -p 6`：两组 tag vet 通过，全量非 race `PREFLIGHT PASS`。

## 提交

- 原 8 个提交（rebase 到 go@1e43a4766 后）：2056033c3 … 95508a151。
- 复核修复：93a95b533 `go(pyref): declare textx for oxidns references, drop deleted convert/ipaddr.go; metadata re-recorded (entries unchanged)`。
- 本报告单独一个提交。
