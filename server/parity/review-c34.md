# C34 独立复核

状态：**56个常规方法复核通过，4项发现（2 high / 2 medium）已修复，新增未结发现0；3个artifact方法按既定合同保持503/wip。**分支 `go-review-c34`，起点为作者
`go-port-c34@2395a3dbb`，基于 `go@215ab6b049`；复核者不是作者。
所有测试、格式检查、vet 均通过 rt 在测试服务器执行，只用合成数据。
无子 agent、无 push、无 PR、无生产访问，未运行完整 gate。

## 发现与修复

- **F1，high：三个 profile 元数据出口及改绑冲突诊断泄露凭据。**
  `repo.AgentBindingSummary` 投影仍保留 Agent 的 host；旧记录的
  `operator:synthetic-binding-password@192.0.2.9` 从 `GET /api/profiles`、
  `GET /api/profiles/<id>/agents` 和成功的 rebind 原样返回。全局 hook 只知道持久化 token，
  不能保护 host 内密码。繁忙 Agent 的名称为 `password=synthetic-binding-error` 时，409
  message 也原样泄露。新增 `TestReviewC34BindingCredentials` 修复前四个出口均失败。
  修复复用 `redact.SanitizeCredentials` 处理这些公共元数据及 DeploymentConflict JSON；
  不改变仓库中的连接信息，回归同时证明原 host 保留、拒绝改绑时 DB/revision 不变。
  修复提交：本次代码修复提交（最终 SHA 见文末）。

- **F2，high：非布尔的有效 dry_run 变成真实导入。**
  Python 对 `dry_run: "yes"`、1、非空列表/对象均只解析并返回200，不修改 DB。
  Go 使用 `GetBool`，把这些输入当 false，实际创建 profile 并返回201；重试随后409。
  `TestReviewC34ClientImportDryRunShapes` 通过真实 Flask 证明修复前该差异。
  修复在既有 legacy repo adapter 中复用现成布尔判定，不新增 Python 模拟 helper；
  五类有效 dry_run 均对照完整响应及完整 DB/revision/文件，必须无写入。
  修复提交：本次代码修复提交（最终 SHA 见文末）。

- **F3，medium：null/缺失 body 被五引擎统一提交，违背本次维护者澄清。**
  原专门 Noop 分支对五个引擎一律200、revision+1。维护者本次明确撤销统一语义，
  要求逐引擎匹配 Flask。独立用非空已保存配置，分别发送 null、空 JSON body、
  不带媒体类型的空 body 和显式 `{}`，20组均显式断言状态、所有业务字段、revision、文件。
  修复前13组差异：四引擎缺失 body、Mihomo/MosDNS null、OxiDNS null/缺失均误提交。
  移除 Noop 分支；Surge/Loon null 使用原来的正常空补丁，其余按下表返回失败且不写入。
  `TestReviewC34CustomEmptyBodies` 修复前失败31.454s，修复后通过33.480s。
  已同步 PROGRESS-config 的 C34 Clarified、作者交回文件的补充说明和 behaviour-changes。
  修复提交：本次代码修复提交（最终 SHA 见文末）。

- **F4，medium：profile 空值默认及导入订阅选择被意外收窄。**
  `objectBody` 只列举字面量0/0.0，合法零值 `-0`、`-0.00`、`0e0`、`0.0e5`
  在 Python 是空更新200，Go500；同一 helper 也用于创建/复制等入口。
  `default_subscription_ids` 的 false/0/空字符串/空对象在 Python 默认为空列表，
  列表中的无法匹配订阅的非字符串成员被忽略；Go 均直接400。另 PUT/clone 的真值非对象在 Python 由仓库返回400，Go适配层却500；现在交给原仓库校验并保持其中文消息。最后确认 clone_from 的非字符串真值会被静默丢掉并创建空profile（Go201 / Flask400），现用共享ID校验拒绝，正常字符串克隆保留。
  `TestReviewC34ProfileEmptyAlternatives` 修复前用真实 Flask 复现状态及 revision 差异。
  零值使用原生 json.Number/strconv 判断；复用已有默认判定并过滤不可能匹配的成员，
  importer 的 Parse/Build 及仓库原子提交协议不变。
  修复提交：本次代码修复提交（最终 SHA 见文末）。

low：未发现需单独修复的低严重度功能问题。另补强认证与别名分派覆盖，见下。

## null / 缺失 / 显式对象的真实 Flask 结果

每组从已保存 `# retained synthetic draft` 开始。空 body 分别检查带
`Content-Type: application/json` 和完全不带 Content-Type，两者结果相同。

| 引擎 | JSON null | 空 body | 显式 `{}` |
|---|---|---|---|
| Mihomo | 500，字段/revision/文件不变 | 500，全部不变 | 200，清空 custom_config，revision+1 |
| Surge | 200，字段/文件不变，revision+1 | 500，全部不变 | 200，字段/文件不变，revision+1 |
| Loon | 200，字段/文件不变，revision+1 | 500，全部不变 | 200，字段/文件不变，revision+1 |
| MosDNS | 500，字段/revision/文件不变 | 500，全部不变 | 200，清空 custom_config，revision+1 |
| OxiDNS | 400，全部不变 | 400，全部不变 | 400，全部不变 |

**null/缺失没有意外清空用户配置，因此未触发维护者要求的停工裁定条件。**
显式 `{}` 的清空是单独验证的既有 Python 行为。非 null 的其他非对象 JSON 仍统一400、
零写入；合法对象各字段原样交给业务验证器。严格畸形 JSON/媒体类型400/415保持 N8 已定合同，
本表空 body 的500是维护者明确要求恢复的特定边界，不复制 Python 内部异常文本。

## 八项重点审计

1. **HEAD**：collection/item/custom 五引擎全部按 GET 分派。既有
   `TestProfileHEADIsReadOnly`、`TestCustomHEADAndBodyBinding` 与真实 Flask 预期差异用例
   比较 GET headers/空响应体、完整 DB（含 revision）、业务文件及预置生成缓存；没有新增写侧分支。
2. **请求形状**：除上表20组，新旧12形状表、对象字段值、原生绑定、五引擎 base_url
   十种类型表全部保留。base_url 缺失/null无覆盖、字符串使用、其他400，非法值在两个下载
   前缀及预览中均不得提交 DB/files/cache。profile/generate 不套用 custom 的非对象新 schema。
3. **导出与凭据**：完整备份使用 auth.Require + AsAttachment 的 File 通道，保留可恢复凭据；
   share ExportAll 必须有全部脱敏 hooks，去掉 Agent/backup/节点/订阅/OxiDNS 私密字段，
   OxiDNS pinned upstream 的地址遮盖且规则身份保留。普通 JSON 仍经 httpx 的已知凭据策略，
   管理员配置编辑器保持既有可编辑内容策略，不擅自全局遮盖用户配置。
   ExportAll(true) 在 WithRepository(ctx,r) 的 WithReadSnapshot 内完成文档复制及凭据收集，
   ctx-first hook 未丢；`TestExportSanitizerUsesExportRepositorySnapshot` 同时覆盖另一仓库绑定、
   已从导出移除的 Agent token 和完整备份绕过 hooks。C34 没有新增业务日志；改绑409的凭据回显已修。
4. **写 owner/Runtime**：global write、profile mutate、saveCustom、configgen.Build 在 owner 内
   Detach，检查 Runtime 取消，再交给共享提交 owner。实际 Runtime.Start/BindRequest/Shutdown
   的 global/import/reset、profile create、五引擎 custom 和五引擎生成覆盖关机拒绝与请求断开完成；
   Surge/Loon null 空补丁保留真实 Runtime 覆盖。下述共享最终提交窗口仍需 O3-6c，未冒称已修。
5. **DNSLoop**：完整对照 Python mihomo_dns_loop 及解析分支，只访问参数，标准 URL/IP 解析，
   无 I/O、仓库、运行时或 onboarding 状态。保留 Python LIFO 扫描顺序、七个键、system、
   IPv4/IPv6/mapped 地址、host的53/1053及无效端口拒绝；原509组真实 Python 对照复验。
   共享 `gateway.MihomoDNSLoop(*yamlx.Node, []netip.Addr)` 可供 A21 直接调用，旧 hook仅做类型适配。
6. **18个外部别名**：原完整 server 注册冲突测试保留。新增
   `TestReviewC34ForeignAliasDispatch` 使用真实 server.NewApp、真实路由匹配及 profile context，
   在业务 handler 前读取最终 Endpoint/ProfileID，逐一断言15个路径的18个方法交给原模块，
   不用缺省404/401来代替分派证据；无重复注册。
7. **3个 artifact入口**：既有注册回归逐引擎断言503及no-store，未知 service400、未知action404；
   确认没有临时复制 A14 发布实现。成功冻结产物留给 A14，不宣称这3方法已成功实现。
8. **59方法认证**：原缺省凭据59方法拒绝矩阵保留。新增
   `TestReviewC34AuthenticationMatrix` 对完整应用的59方法逐一检查 config/rule/retired/Agent/wrong
   Bearer；相同五种 query token 检查管理入口，只有11条 live配置允许 config query token。
   所有拒绝请求结束后比较完整 DB；另逐一以有效 JWT 进入全部59方法，业务失败允许400/404/409，三个artifact必须503。live config token 的11个成功路径由已有边界矩阵复验。

## 范围与外部分工

- 已完整读取 Python generate.py、config.py、profiles.py、common/config_export.py，逐分支对照
  C34 adapters、configgen、configexport、共享 DNSLoop 与 repo导出 hook；核对作者测试映射。
- 已读 AGENT-BRIEF、PORTING、WORKFLOW、O3及 C29/C33/A24/N8-4ef复核类别。
  本分支不存在 `review-c06.md`、`review-c07.md`，已补读 PROGRESS C06/C07和实际 auth/httpx/redact出口，
  不把未提供的报告写成已读。
- A11 Previews 的 keyed leaf lock/关机后检查按已有协调裁定等待 O3-6c，不在此复制实现。
  共享 repo最终SQLite/file rename、MosDNS archive/rulefetch/taskstatus 的取消闸门已有 O3-6c owner，
  此基底尚未合入；本轮入口拒绝测试不能代替共享锁等待后及最终提交前的取消验证。
- A14仍负责3个 artifact成功路径、frozen-byte/tamper/stale/expired；C34保留明确503。
- 无新增依赖、共享接口变更、私有业务 helper副本；未改 backend/frontend。未改变操作入口，无新截图需求。

## 定向验收

普通 API/profiles/generate及启用的整组 C34 Flask差分已通过，最后输出：

```text
ok configflow/internal/api/profiles        5.358s
ok configflow/internal/api/generate        3.741s
ok configflow/internal/api/config/difftest 102.931s
```

最后定向验收固定 **192.168.0.64**，退出0：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api -- bash -lc 'set -e; cd server; gofmt -w internal/api/config internal/api/profiles internal/api/generate/review_test.go; test -z "$(gofmt -l internal/ tools/ cmd/)"; go vet ./...; go test -race -p 4 -count=1 -timeout=15m ./internal/api/config ./internal/api/profiles ./internal/api/generate ./internal/configgen ./internal/configexport ./internal/server; go test -race -count=1 ./internal/repo -run "Test(ExportSanitizerUsesExportRepositorySnapshot|DesensitizedExportNeedsRedactionHooks)$"; go test -race -count=1 -v ./internal/gateway -run "Test(DNSLoopMatchesPython|Hooks)$"; CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=15m ./internal/api/config/difftest'
```

```text
ok configflow/internal/api/config        19.325s
ok configflow/internal/api/profiles       9.358s
ok configflow/internal/api/generate      16.215s
ok configflow/internal/configgen          4.411s
ok configflow/internal/configexport       1.090s
ok configflow/internal/server             2.165s
ok configflow/internal/repo               2.822s
pure DNS loop Python comparisons=509
ok configflow/internal/gateway            1.538s
ok configflow/internal/api/config/difftest 164.890s
```

gofmt条件与全仓vet无输出。最后补充数字字符串负控时真实Flask还揭示PUT/clone真值非对象的400边界，
已交回仓库原校验；该最终小改动及新增用例再定向race通过：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/profiles/profiles.go --back server/internal/api/config/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/profiles/profiles.go internal/api/config/difftest/review_test.go; go vet ./internal/api/profiles ./internal/api/config/difftest; go test -race -count=1 ./internal/api/profiles; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/config/difftest -run "^TestReviewC34(ProfileEmptyAlternatives|ClientImportDryRunShapes)$"'
```

```text
ok configflow/internal/api/profiles        9.781s
ok configflow/internal/api/config/difftest 9.355s
```

认证矩阵最后加入全部59方法的有效JWT进入控制（包含3个artifact503），在 **192.168.0.32** 补验：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/api/generate/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/generate/review_test.go; go vet ./internal/api/generate; go test -race -count=1 ./internal/api/generate -run TestReviewC34'
```

```text
ok configflow/internal/api/generate 16.721s
```

没有修改parser或新增fuzz目标，既有configexport fuzz不因本轮adapter修改重复执行；作者60s记录保留。
修复前回归均通过rt执行并失败：CustomEmptyBodies (.64，31.454s)、BindingCredentials (.32，0.619s；
补充409凭据回显 .64，0.658s)、ClientImportDryRunShapes (.64，1.687s)、ProfileEmptyAlternatives
(.64，3.995s)，以及非法clone_from（.32，2.378s，Go201 / Flask400）。未放宽oracle或原归一规则。

补充的存储文本断言防止 YAML 结构归一把注释草稿与空文本视作相同：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/config/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/config/difftest/review_test.go; go vet ./internal/api/config/difftest; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/config/difftest -run "^TestReviewC34CustomEmptyBodies$"'
```

结果：`ok configflow/internal/api/config/difftest 70.200s`。20组同时逐字节断言保存的custom_config，
因此null/缺失无清空的结论不依赖YAML语义归一。

最终clone_from校验及四类非法来源/正常克隆对照补验（.64，退出0）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/api/profiles/profiles.go --back server/internal/api/config/difftest/review_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/api/profiles/profiles.go internal/api/config/difftest/review_test.go; go vet ./internal/api/profiles ./internal/api/config/difftest; go test -race -count=1 ./internal/api/profiles; CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/config/difftest -run "^TestReviewC34(ProfileEmptyAlternatives|ClientImportDryRunShapes|InvalidCloneSourceCannotCreateProfile)$"'
```

```text
ok configflow/internal/api/profiles        7.491s
ok configflow/internal/api/config/difftest 10.881s
```

新增测试文件：config/difftest/review_test.go、generate/review_test.go、profiles/review_test.go。
修复提交与最终变基记录在交回提交补齐。
