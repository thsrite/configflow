# N8-11 W1 repo typed 补全 HR 复核（fresh Opus）

复核对象：`go-n8-11-w1-repo-typed`，只看 `go-n8-11-p0..4009d59be` 的两个提交（eff5f6e78 typed 实现、4009d59be 补强测试与验收报告）。
P0（testjson、棘轮门禁）不在范围内。复核分支 `go-opus-review-n8-11-w1`。对照的是 Python（`backend/common/config_repository.py`、
`backend/common/config.py`）和 P0 基底上的旧 Go（`git show go-n8-11-p0:server/internal/repo/...`）。协调者已经定下的裁定（最终 API 名、
legacy.go 只做转码、typed hook 优先且只触发一次）只核对了实现，没有重新讨论。定向测试只在 .64 上经 `rt` 运行，没有重跑作者的整套差分，
也没有跑完整门禁。回归测试只用合成数据。已知非 W1 问题（全局 import/reset 的 409 多带 usages，交 W5）不计入。

## 结论

high 0，medium 1（已修，附回归测试和负控），low 4（记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **令牌脱敏热路径会编码整条 Agent，包括发布历史。** 位置 `repo/system.go` `sanitizationTokensModel`。W1 把 `system.Agents[i].MemberJSON("token")` 和 `SystemConfig.MemberJSON(...)` 改成了 `recordMemberJSON(...)`。`recordMemberJSON` 会先 `model.Marshal` 整条记录，再取其中一个成员。`model.Agent.MemberJSON` 的注释写明它是给热读路径用的，“without encoding the rest of this Agent or its history”。这个函数被 `redact`（日志和响应脱敏）、`mosdns/ruleproxy`、`mcp/transport`、`dnsha` 通过 `RuleProxyTokensForSanitization/AgentTokensForSanitization` 调用。在事务内、`lockedSnapshot` 内，以及每次提交出现新 head 时，它都不走缓存。修复前，一个带 2 MB 发布历史的 Agent，每次调用要分配约 56 MB；修复后约 60 KB。修复：三处改回 `MemberJSON`，返回的令牌集合不变。测试 `TestReviewN811SanitizationTokensSkipAgentHistory`：断言规则代理的当前令牌和退役令牌、Agent 令牌都能取到；在 1 条历史和 2000 条（约 2 MB）历史两种情况下比较每次调用的分配字节，要求两者之差不超过 64 KB。负控：在服务器副本上把 Agent 那一行改回 `recordMemberJSON`，测试失败（`56629759 bytes … (59610 with a small one)`）；还原后通过。 |
| L1 | low | 记录（交 W3/W5 简报） | **请求形状校验只在 legacy facade 里有。** 位置 `repo/legacy.go`：`UpdateProfileMetadata`（只允许 name/description、值必须是字符串）、`profileCreationFromLegacy`（id/name 的真值判断和类型检查，description 按 `jsonx.Str` 转成文本）、`deletionIDsFromLegacy`（`请选择要删除的节点`）、`ImportClientProfile` 对结果形状的检查。typed API 已按裁定收 DTO（`ProfileMetadataUpdate`、`ProfileCreation`、`[]string`），所以这些 400 只能由调用方在 HTTP 层保留。这不算 facade 自带业务逻辑，但 W3（nodes 删除）和 W5（profiles）迁移时必须把同样的校验放进请求绑定，并跑对应 Flask 差分（错型 name/description、多余键、非列表 ids），否则会漏掉 400。 |
| L2 | low | 记录 | **`SaveProfileFieldsJSON` 靠指针别名拿到保存后的 profile。** 位置 `repo/profile_records.go`。它在 `UpdateConfig` 回调里从工作文档取 `profileFor(document, id)` 指针，依赖 `updateProfileIn` 之后对同一个结构做 `*current = *working` 和 `Revision++`。当前结果正确（探针确认：返回的 profile revision 等于存储值，返回值 = profile + shared revision），但这依赖内部实现细节。建议改用包内的 `r.updateConfig`，它本来就返回 saved 和 revision；留给 W9 或下一次改 repo 时处理。 |
| L3 | low | 记录（W9） | **repo 非 legacy 文件里还有 jsonx。** `router_guard.go`（`storedValue` 和路由器 409 usages 的 `jsonx.Obj`）、`sections.go` 中 `updateProfileIn` 的“仍引用被删除策略组”usages、`errors.go`/`lock.go` 的 `jsonx.Repr`。这些文件不在计划 §2.3 的 W1 列表里，棘轮清单也有登记，不算违规。不过 `resourceUsages` 已经改用 typed `Usage`，同一个 `Error.Usages` 现在混着两种元素。W9 删除 jsonx 时要一起改。 |
| L4 | low | 记录 | **`_revision` 冲突比较从 jsonx.Equal 换成了 `sameStored`。** typed 的 `SaveSharedRecord/SaveSystemRecord/ImportProfileJSON/SaveProfileFieldsJSON` 在没有 baseline 时，用 `sameStored`（yamlx.Equal，`1` 和 `1.0` 不相等，null 和缺失视为相等）比较提交的 `_revision`。旧 Go 用 `jsonx.Equal`，按 Python 数值相等。只有手工构造、`_revision` 写成 `3.0` 的请求会从“通过”变成 409；前端只发整数。属于“不再追的 Python 等值细节”，本轮不改。 |

## 逐项核对

1. **存储字节兼容。** 当前格式导入走 `model.DecodeDocument`，再经 `Clear` 和校验后整体存储，不经过 jsonx 或 float64。作者的负控只覆盖文档根层的未知成员；我另加探针（没有提交）：节点内的未知成员和 profile `oxidns` 内的未知成员都包含大整数、指数小数和 `\u` 转义，导入后导出，逐字节出现 2 次，保留成功。之后 `UpdateConfig` 修改 `device_notes`，这两处字节仍然不变。`decodeEntry` 不再预先生成 legacy 文档。`fileJSON` 改用 `json.MarshalIndent`（会转义 HTML 字符，值相等），已登记在 behaviour-changes。
2. **导入/导出/重置。** 脱敏导出逐项对照了 Python `export_all`：三个敏感设置、backup/agents、订阅 url、节点五个键加 params、oxidns/routers/easytier 三个 hook、三个引擎和 loon 的 custom_config、restore point，最后交给外部 sanitizer，传入的是原始 settings。`settings := document.System.SystemConfig` 是值拷贝，`ClearMember` 先 clone 再改，所以 sanitizer 拿到的仍然是带令牌的原值。hook 缺失时拒绝导出（`msgExportHooksMissing`）。导入仍然先拍快照、走 batch、退役旧令牌、所有 revision 推进，与旧实现相同。
3. **三路合并和 Scope baseline。** `SaveSharedRecord`/`SaveSystemRecord` 与旧 `SaveShared`/`SaveSystem` 一致：有 baseline 时不查 revision，按成员 `MergeJSON`；没有 baseline 时查 `_revision`，system 对象深合并。探针确认 `model.Marshal` 对缺失成员不输出，所以 legacy 部分快照不会覆盖未提交的成员。包级 `SharedConfig` 把基线存进 `recordBaselines`，`bind` 或成功保存后清空。作者测试覆盖了两个 Scope 并发编辑都保留，以及保存后用旧快照再存会得到 409。
4. **UpdateConfig/UpdateSharedConfigs。** 对照 Python `_update_profile_locked` 和 `update_shared_and_profiles_transaction`：selector 先于 before 解析，shared 先过删除守卫，再逐个编辑 profile；删除策略组的引用检查和 ruleset 源字段剥离仍在 `updateProfileIn` 里；每个 section 的 revision 只推进一次；任一步失败整笔回滚。`changedMembers` 只写有变化的成员；探针确认回调把 `ProxyGroups` 设成 nil 或空切片时，`[]` 能正确落盘，不会被当成“未变化”漏写。
5. **锁和 ctx（第四条）。** 新写入口（`UpdateConfig/UpdateSharedConfigs/SaveSharedRecord/SaveSystemRecord/Create*/ImportProfileJSON/ImportClientProfileRecord/SaveProfileFieldsJSON/DeleteNodesWithReferencesIDs/ImportAllJSON/ResetAllJSON`）都先 Detach，再进入 `transact`：开始时检查 `ctx.Err()`，最后经 `CommitContext` 提交。三条获准的 filesystem+DB batch 没有增加。作者用真实 `Runtime.Start/BindRequest/Shutdown` 测了 UpdateConfig 和 UpdateSharedConfigs 的失败、关机、断线三种情况。
6. **legacy facade 和 hook。** 旧入口都改成转码后委托 typed 实现（`UpdateProfileTransaction`、`UpdateSharedAndProfilesTransaction` → `updateConfig/updateSharedConfigs`；`SaveShared/SaveSystem/ImportProfile/UpdateProfileFields/PreviewNodeDeletion/DeleteNodesWithReferences/CreateRuleLibraryItem/ExportAll/ImportAll/ResetAll`），只剩 L1 所说的请求形状转换。`UpdateSystemTransaction` 和 `UpdateSharedTransaction` 仍然是 jsonx updater 加 `setChanged`/`editLegacyMembers`，这是旧 Updater 签名的转码，没有对应的 typed 业务。repo 内的 hook 读取都改成了 `recordHooks()`（router guard、commit observer、节点删除、脱敏），只有 `scope.go` 的 `InitAgentManager` 还用 `currentHooks()`（与本包无关）。typed 和旧 hook 同时注册时只包一层，不会触发两次。旧 `PlanNodeDeletion` 的预览经 JSON 往返转成 `NodeDeletionPreview`，字段与 `api/nodes/deletion.go` 的 `deletionPreview` 完全一致。
7. **defaults。** `oxidns_defaults.json` 与 P0 的 `GetDefaultOxidnsSettings` 逐键一致；`defaultConfigJSON` 与旧 `GetDefaultConfig` 的键序一致；`InitialConfig` 用 `mergeObjectsJSON`（保持 base 顺序，新键追加），与 `config._deep_merge` 相同。`TestReferenceLegacyDefaults` 从文本比较改成了结构比较，属于已批准的成员顺序许可。
8. **节点删除。** 计划交给协作者，拿到的是文档的拥有副本。没有 blockers 时才校验候选文档，`ErrRepository` 转成 blocker。确认摘要用常量时间比较。写回 profile groups → aggregations → nodes，节点仍然经过 shared 删除守卫。与 Python `_node_deletion_plan` 和旧 Go 一致。
9. **测试替身和负控。** hook 假实现只测 hook 契约，没有覆盖生产参数。字节负控（float64 重编码会改变夹具）、Runtime 三种模式、Scope 重置后 409，都不会空过。回放比较只放宽了随机 ID 后缀、`proxy_string` 解析后比较、物理 position/counter，并另外加了“被拒操作前后行指纹不变”。

## 复核运行（.64）

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/repo)" && go vet ./internal/repo && go test -count=1 -run "TestReviewN811|TestN811|TestReviewAgentInspection" ./internal/repo && echo "== negative control: encode whole Agent" && cp internal/repo/system.go /tmp/zzsys.go && sed -i "s/system.Agents\[i\].MemberJSON(\"token\")/recordMemberJSON(\&system.Agents[i], \"token\")/" internal/repo/system.go && go test -count=1 -run TestReviewN811 ./internal/repo; cp /tmp/zzsys.go internal/repo/system.go && echo restored && go test -count=1 -run TestReviewN811 ./internal/repo'
```

```text
ok  	configflow/internal/repo	3.035s
== negative control: encode whole Agent
    review_n811_test.go:59: token collection allocates 56629759 bytes with a large history (59610 with a small one)
FAIL	configflow/internal/repo	0.742s
restored
ok  	configflow/internal/repo	0.117s
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/repo)" && go vet ./internal/repo ./internal/redact && go test -race -count=1 -timeout=30m ./internal/repo ./internal/redact'
```

```text
ok  	configflow/internal/repo	333.070s
ok  	configflow/internal/redact	1.347s
```
