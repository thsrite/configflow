# C29 hand-back

状态：实现完成（ported，未标 verified）；已补第四常设项，完整 gate 待跑，最终结果待填。
范围：Python oxidns_native_apply.py、oxidns_archive.py、routes/oxidns.py 和 profile wrappers，共18路由。
旧 go-n8-4ef worktree 未修改。本分支 go-port-c29；已 rebase go@e980d402。

## 实现与证据

- Native apply 用 typed rename/standard-add 意图，复用共享 common/routing/withdrawal/reference 引擎；
  不猜测插件类型或列表位置作为身份，原始设置/额外成员不被未请求的编辑改写。
- Archive 复用 mosdns.ArchivePath / BuildMosDNSZip；配置与附件只读，校验 state/include 和全部资源引用，
  ZIP 内原始附件包括二进制保持字节，资源下载/转换/去重/预算由 C28 owner 负责。
- HTTP18条 method/path 已注册且唯一；N8-6 BindSilent 对应 Python silent JSON；typed DTO/model.Dynamic；
  写 owner 取锁前 Detach，事务内按 fresh config 校验 If-Match 与 invalidate_follow；一次返回 ETag/revision guard。
- 222条真实 Python native apply 边界：settings/common/notices、错误中文、结构化 references 和输入 post-state。
  录制驱动170个真实 pytest cases（7个API cases不作函数记录，HTTP workflow接管）。
- 18组 ZIP 对照：每个成员的路径、顺序、权限、内容；含本地转换回调、重复来源仅获取一次与原始 AdGuard。
- 实时 Flask↔Go：每条路由明确200成功和失败，完整 DB export + 所有数据文件逐请求对照；
  全局/profile/选择配置、凭据保留、功能关闭恢复、不完整草稿与发布校验、引用失败、JSON/media、If-Match。
- 并发相同 If-Match 两次保存只成功一次；另一个409且revision不增；取消请求仍写完且保留并发 MosDNS 字段。
- HANDOFF3c 全路由注册冲突与完整 app 的精确状态/dispatch 回归。

Python shared tests download_aliases/withdrawn_downloads 继续由 C25–C27 既有边界覆盖；最终重跑整个 oxidns 包。
集成测试涉及的备份/clone/import、Agent投递与MCP仍由各自已移植owner承担，C29没有复制这些实现。

## 已批准差异与限制

- HEAD只读，GET同状态/全部headers、空body；带JSON不写DB/文件/revision。隔离Python副本明确复现旧保存bug。
- N8-6/§5.7 非有限JSON：16个POST pair各3条，48个显式差异向量；Go响应逐字段对照真实Flask malformed-body控制，
  全部存储保持不变。合法JSON和原有业务错误严格比较。
- 两个共享reader单独提交：yamlx.Parse/D1/tag-shape/merge/alias预算；只有ErrSyntax保留旧失败契约。
  业务builder和validator不动；include错误仅保留reader cause。40层alias、自引用、重复键等安全回归。
- 迁移逐输入occurrences：native53588 = equal52768 + D1 547 + policy177 + alias96；
  template326232 = equal325967 + D1 132 + alias133；两者unknown=0。详情 progress/n7-3d-oxidns.md。
- 除上述差异，无已知C29普通运行功能差异或未移植项；共享shutdown窗口见下；无新依赖、未push、未开子agent、未操作生产。
- N8-5后续位置：settings_store.go 的storedSettings/事务赋值；api/oxidns的render config/NewProfile桥；
  archive.go现有共享MosDNS config参数。当前沿用已批准repo API，新设置载体已用model.Dynamic。

## 关机自查与共享剩余窗口

SaveSettings和BuildZIP均在Detach后及调用提交/归档前检查ctx.Err；SaveSettings锁内callback还在交回候选前检查。
真实Runtime.Start/BindRequest/Shutdown回归比较完整DB/files/commit count/revision，并验证请求断开仍完成设置保存与完整ZIP。
.64 scoped race已通过（2.811s）；同轮API 0.240s / Flask差分11.494s。

按协调者最新分工，共享层不由C29修改：mosdns/archive的WithoutCancel、taskstatus RecordContext/ForgetContext、
repo最终提交和rulefetch touch/rename取消检查，统一由6e5c7760（go-o3-6）在O3-6a之后的
O3-6c “shared commit points honour shutdown”步骤补齐。C29入口已保护，已进入共享链的在途请求仍有此已知窗口。
O3-6a合入后再只读复查链路并更新本项。

## 命令与输出

.64 针对性测试：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -timeout=10m ./internal/api/oxidns/... && go test -count=1 -run "^TestC29" -timeout=10m ./internal/oxidns'
```

初次末行：api/oxidns 0.085s；api/oxidns/difftest 4.036s；oxidns 3.776s。
补48条非有限向量与18条成功断言后，完整API差分末行 `ok .../api/oxidns/difftest 8.530s`。
reader最终逐行门：`go test -count=1 -run "^TestN73OxidnsReaderInputs$" -v -timeout=15m ./internal/oxidns`，
末行 `PASS; ok configflow/internal/oxidns 56.795s`。

最终命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'server/scripts/gate.sh -p 6 FuzzNativeYAMLRoundTrip:./internal/oxidns FuzzZoneValidation:./internal/oxidns'
```

为避免轮询饥饿，实际rt命令先阻塞等待现有slot 2再立即exec上述gate，完整命令见progress/c29.md。
待记录：/tmp/c29-gate-e980d402，gofmt/vet/build tags、全量race、proddiff、全部difftest、两fuzz各60秒。

## 提交

当前实现提交：bc5c7856 reader；0995742a native apply；415dbd45 archive/routes；c924454d N8-6向量；ed428ce2验收checkpoint。
提交可能随最终rebase变化，以最终交回记录为准。

## 新文件（相对 server）

| 路径 | 规模 |
|---|---:|
| `internal/api/oxidns/difftest/diff_test.go` | 535 lines |
| `internal/api/oxidns/difftest/testdata/reference.py` | 52 lines |
| `internal/api/oxidns/messages.go` | 6 lines |
| `internal/api/oxidns/oxidns.go` | 282 lines |
| `internal/api/oxidns/routes_test.go` | 93 lines |
| `internal/oxidns/archive.go` | 95 lines |
| `internal/oxidns/archive_test.go` | 210 lines |
| `internal/oxidns/n73_legacy_test.go` | 126 lines |
| `internal/oxidns/n73_readers_test.go` | 223 lines |
| `internal/oxidns/native_activate.go` | 357 lines |
| `internal/oxidns/native_apply.go` | 633 lines |
| `internal/oxidns/native_apply_test.go` | 75 lines |
| `internal/oxidns/native_input.go` | 67 lines |
| `internal/oxidns/native_response.go` | 18 lines |
| `internal/oxidns/reader.go` | 29 lines |
| `internal/oxidns/settings_store.go` | 148 lines |
| `internal/oxidns/settings_store_test.go` | 111 lines |
| `internal/oxidns/testdata/c29_archive.py` | 26 lines |
| `internal/oxidns/testdata/c29_gen.py` | 85 lines |
| `internal/oxidns/testdata/c29_native.jsonl.gz` | 479251 bytes |
| `parity/progress/c29.md` | 75 lines |
| `parity/progress/n7-3d-oxidns.md` | 30 lines |
