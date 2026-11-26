# batch-46 HR 复核（O17 1.8：RouterOS 行文本化、iKuai ikuaiRow、iKuai 新建表单结构体）

分支 `batch-46`（= go-o17-typed-ros，4 个提交），复核前 rebase 到 `go@ffd33e872`（无冲突）。
复核修复提交：`50a77a404`。协调者裁定第 ③ 步不做，本次不涉及。

## 方法

逐个读取点对照 `go@ffd33e872` 的旧实现（`rosText`/`rosTextDefault`/`firstText`/`rosTruthy`/`clientTrue`/
`valueOr`/`cloneAddressRow`/`truthyValue`/类型 switch），看 null、数字、布尔、数组/对象四类值在新旧实现里
走到哪个分支；重点是原来「阻断/未知」的点现在是否变成「放行/不匹配」。写入侧对照发出的 JSON。

## 发现

### 中（已修，`50a77a404`）

1. **写路径上 IPv6 ND / DHCP 网络行的 null 被当成缺失 → 允许接管/恢复。** `strictList` 只覆盖了只读分析
   （`readIPv6DNS`、`ReadDHCPDNS`），而 `payload`/`payloadError` 读的行来自 `list`/`find`
   （`perform`、批量恢复、PD 计划、NAT 写入）。旧实现：ND 的 `disabled`/`dynamic`/`invalid` 为 null →
   `ipv6DNSFlag` 报错 → `payload` 为 nil → 拒绝（「IPv6 DNS 下发的 … 状态无法识别」）；DHCP 网络
   `dns-none` 为 null → `dhcpNetworkAttributes` 报错 → 拒绝。改后 null 被丢弃，按 "false" 处理 → 放行。
   修法：`list` 对每张表的 `rosNullFields`（ND 三个标志、DHCP 网络 `dns-none`、`/interface/list` 的
   `include`/`exclude`）保留 `nullMarker`；`strictList` 与之合并为 `listMarked`。
2. **`/interface/list` 的 include/exclude 为 null 或非文本 → 策略接口匹配从「未知」变成「不匹配」。**
   旧 `policyInterfaceBounded.listMatch` 对非字符串（含 null）直接返回 `PolicyUnknown`，这段检查被删了；
   null 现在是缺失 → 空列表 → `PolicyNoMatch`，可能让一条本应「不确定」的规则被判不命中（R11 就绪、
   转发提示用到）。修法：同上保留 null 标记，`listMatch` 遇结构化文本返回 `PolicyUnknown`。
3. **`MatchPolicyRule` 遇数组/对象值 → 从「未知」变成按文本匹配。** 旧实现 `rosText(数组)` 为 "" → 未知；
   现在是 JSON 文本，`protocol`/`in-interface`/`connection-state` 等会得出 `PolicyNoMatch`。修法：结构化
   文本直接按未知处理。

三项都由新测试 `o17_review_test.go` 固定（把修复回退后测试失败，恢复后通过）。

### 低（记录，不改）

4. **RouterOS 路由编辑器：null 字段不再使路由只读。** 旧 `editROSEntry` 对 null（及非整数 JSON 数字）
   报「路由配置字段格式无法识别」，null 的 `comment` 报「路由备注格式无法识别」。现在 null 是缺失，路由可编辑；
   补偿恢复时不再写回该字段（旧实现会写 JSON null，RouterOS 也不会接受）。这与「null = 缺失」（方案 A）一致；
   不能用全局标记修：路由行的 `routing-table` null 原来按 main 处理，标记会让重复/外来路由检查漏判，反而放行。
   RouterOS REST 不发 null。
5. **`Row.flag` 对 JSON 数字 1 现在为真**（旧实现只认字符串 "1"）。只影响 `disabled` 等标志为数字 1 的输入，
   真机不发；语义上反而更对。
6. **`validateAddressEntry` 不再拒绝数组/对象值。** 入参是 Go 代码生成的地址条目（全文本），控制台参数仍经
   `ScriptString` 转义，无注入面。
7. **`nullMarker`（"{}"）会出现在 DHCP 预览证据里**：未被读取的行（如禁用租约的 null `dhcp-option`）原来证据里是
   null，现在是 "{}"；接口列表的 null include 在 NAT 评估（`listContains`）里现在判「不确定」（原来当未设置）——
   都是更保守方向。
8. 结构化判断基于文本形态：值恰好是 `[..]`/`{..}` 的合法 JSON 文本（如备注 "{}"）会按结构化值处理，只会走更保守
   分支（路由备注标为不可编辑、策略选择器未知）。

### 核对通过（无问题）

- **null 视为缺失的其余读取点**：`rosTextDefault(nil, "main")` 与 `textOr` 一致（null routing-table 仍是 main）；
  `PreservesSources`/`InspectNATListRule` 的 null chain 与缺失同路径；`MatchPolicyRule` 原来就跳过 null；
  `ReadState` 路由 `active: null` 现为 nil（原为 false），只影响展示。IPv6 DNS、IPv4 地址标志/DNS 列表、
  DHCP 选项/DNS 列表、`policy-rules` 的 null 仍按原文案阻断（`strictList`/`markNulls`）。
- **布尔 true 与 "true"**：`rosTruthy`/`clientTrue`/`editROSFlag`/`ComparableValues`/`dns-none` 校验原来都同等接受
  两者，替换后等价；`IPv6EvidenceRow` 只认 JSON 布尔 `active`，存储的旧计划可解码。
- **删掉的辅助函数**：`rosText`→直接取值；`rosTextDefault`→`textOr`；`firstText`→`firstNonEmpty`；
  `rosTruthy`/`clientTrue`→`rosFlag`；`valueOr`→`textOr`；`cloneAddressRow`→`cloneRow`；`truthyValue`→
  `ikuaiRow.filled`。逐个调用点核对等价（数字/布尔转文本后结果相同；数组/对象的差异见上 2、3、6）。
- **RouterOS 写入**：`Row` 只装文本，所有写入构造点原来就是字符串字面量；`/print` 查询体 `rosPrint` 字节不变；
  nil 值无请求体、空 `Row{}` 发 `{}`，与原来一致。
- **iKuai 写入**：修改/恢复仍发 `ikuaiRow`（原值类型、未知字段保留）；DHCP 修改的 before-image 仍按 float64 解码
  （与原 `AttributesRow` 同）。新建 NAT 免转换、端口分流、静态路由的结构体表单与改前 map 表单发出的请求体
  **逐字节相同**：`TestO17IkuaiNewFormsSameBytes` 用改前的 map 构造函数对照（空/字面量/空列表/IP 组引用/nil 引用/
  更新 id 0，含中文、引号、换行、`<&>`），并比较 `IkuaiRequestBytes`（超长判断）。
- **对外 JSON**：预览证据 `active` 仍为 JSON 布尔；`RouteSnapshot` 按厂商编码原记录（nil 为 null，与原来同），只在
  routers 包内使用；proddiff new=0。

## 验证（代码提交 `50a77a404`）

- `.32`：`gofmt -l .` 无输出、`go vet ./...` 通过；`CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout 60m`
  routers/...、agents/...、api/clientdevices、api/config、api/routers/...、api/rules/...、clients/...、
  notifications、routersplit/...、setup 全部 ok（routers 788 s）。
- `.64`：fuzz 各 60 s——routers 13 个（ClientDuration、R09PolicyEvaluator、R09DNSGuardManifest、
  R09DNSGuardOperation、FakeIPYAML、R08PolicyInput、N89StoredRouter、R07DecodeAddressRow、R14Classify、
  R11PDInput、R11DNSRow、R10IPv6Input、R12ReturnHandshake）+ routersplit RouterSplitYAML，全部 PASS。
- `.64`：`GATE_ONLY=proddiff server/scripts/gate.sh -p 6`：baseline new=0 stale=0，self_check PASS，GATE STAGE PASS。
- `.32`：`/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-46 -p 8`：两组 tag 的 vet + 全量非 race，PREFLIGHT PASS。
