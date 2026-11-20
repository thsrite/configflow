# N8-11 batch-16 集成修复

状态：**完成**。修复、定向回归、实时 pyref check、相关包 race、完整预检全部 PASS。分支 `batch-16`，任务起点 `9b29ede168`。只在测试服务器运行测试，不 push。

## 根因、引入提交与修复

- OxiDNS：`a7b2777ca` 修改 `adapter.go` 后，13 个 source 注册测试依赖的 pyref 元数据过期，实际需刷新 7 个文件。协调者追加裁定接受 generation corpus 的 270→266：W2 恢复显式禁用语义后，4 条 `unavailable_rulesets` 已直接匹配原始冻结语料，不再进入 `nativeReference`；不强迫消费无须使用的 reference。
- Oracle 审计：`4831dcd25` 新增 `TestN811GlobalFlaskBoundaries`，未登记其中两个 `exec.Command`（复制合成种子树、启动 Flask oracle）。按 exact file/function/callee、count=2、CONFIGFLOW_DIFF guard 注册，保留审计。
- Provider：`4831dcd25` 将 `fileJSON` 改为 `json.MarshalIndent`，默认 HTML escaping 把缓存 URL 的 `&` 写成 `\u0026`，与原文件断言不符。报错中的 `got null` 是 `diffText(in,nil)` 的诊断前缀，文件仍存在。统一改为 native Encoder + SetEscapeHTML(false) + SetIndent；保持末尾换行。W1 的其他 JSON **文件**写出点已核对：shared/profile 文件经 writeJSONContext→fileJSON，deleted-profile tombstone 也经 fileJSON；其余新增 json.Marshal 用于模型、数据库或回调边界，没有另一个文件编码器。新增 shared/profile 原生对象文件回归，逐字节确认含 `&<>` 的 URL。
- Aggregations 取消：`4831dcd25` 将 legacy ProfileMetadata 委托到 ProfileMetadataRecord→View，提前返回 context.Canceled，导致请求在启动 provider shared flight 前失败。只在 legacy 委托边界 ctxx.Detach；native View/metadata 仍响应取消，work shutdown 仍传递。新增这三种生命周期回归；原路由测试断言不改，修复后连续 10 次通过。

## pyref 差异审查

全部变化均已逐文件比较 metadata 和 entries。只有 `metadata.sources["server/internal/oxidns/adapter.go"]` 的 hash 更新，其他 source、input_sha256、venv_dependencies_sha256 和所有其他 metadata 字段完全不变。

- TestBoundaryGenerationCorpus：270→266，无新增，其余 266 条 input/output 逐项完全不变。
- TestBoundaryGenerationRecorded：5→5；TestBoundaryRoutingCorpus：60→60；TestC05ReviewRecordedNormalization：1→1；TestC05ReviewSettingsReferences_native_19_False / True：各 1→1；TestStoredLegacySettingsAgainstPython：13→13。六文件 entries 的数量、内容均不变。
- 将批次全部改动与 tools/pyref/sources.json 相交，仅 adapter.go 命中上述 13 个注册测试；没有其他漏刷 source。

被移除的 exact input keys（原始 corpus.jsonl.gz **零基**行号）：

| 行号 | 输入键 | 原输入规则的禁用标记 |
|---|---|---|
| 9934 | `9ab540bcddbdd77bb02b9913c0e6b225f0c2c3756d0392696d860e64342eceae` | 最后一个 ruleset r2：enabled=0 |
| 24856 | `67d6d9dfe92c932910adcf9127b0c960464851c35caef563ff9b6dbc3ccf7324` | 第二个 ruleset r1：enabled=0 |
| 26519 | `d1fd68b44116e26221025639a8e96a42d6975359437a91bc3d486e8eda2e88fa` | 最后一个 ruleset r1：enabled=0 |
| 27837 | `e6ab43f589632fd354c8ecf12f2ab75fba7a497b6564c17af48baa152e48351f` | ruleset s1（Set A）：enabled=0 |

旧 exact reference input 经过既有 projectReferenceMetadata 将错误类型 enabled=0 投影为缺失字段；W2 直接保留禁用语义，因此现在直接匹配冻结的“已停用”输出。在 .32 使用临时、仅取证的 Go 测试，对以上四个原始行分别执行原 call + compareFrozen（不走 compare/nativeReference），四条全部直接匹配。临时文件已删除，冻结语料与 oracle 脚本均未改。

## 基线归因（.64）

通过 git archive 将三个版本放到 `~/batch16-baselines/{go,w1,w2}`，各自执行：

```sh
source ~/.rt-env
cd ~/batch16-baselines/<stage>/server
go test -count=1 -timeout 5m ./internal/provider -run '^TestCorpus$'
go test -count=10 -timeout 5m ./internal/api/aggregations -run '^TestRouteFallbackCoalescesAndFinishesAfterCancellation$'
```

| 树 | provider | 取消路由 10 次 |
|---|---|---|
| go=e7aba2586 | PASS，15.228s | 10/10 PASS，2.145s |
| go+W1=546c33a52 | FAIL，180 个相同文件差异，15.229s | 10/10 FAIL，101.194s |
| go+W1+W2=9c061da07 | FAIL，180 个相同文件差异，15.215s | 10/10 FAIL，101.759s |

两处均由 W1 引入、必现；不是 W2 或时序偶发。

## 检查命令与最后输出（.32）

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/repo/batch16_regression_test.go -- bash -lc 'cd server && gofmt -w internal/repo/batch16_regression_test.go && go test -count=1 -timeout 5m ./internal/repo -run "TestArtifactJSONKeepsLiteralURLSeparators|TestLegacyProfileMetadataKeepsWorkLifetime" && go test -count=10 -timeout 5m ./internal/api/aggregations -run "^TestRouteFallbackCoalescesAndFinishesAfterCancellation$" && go test -count=1 -timeout 5m ./internal/provider -run "^TestCorpus$" && go test -count=1 ./tools/pyref -run "^TestAuditRepository$"'
```

退出 0：repo 0.200s；aggregations 2.609s（10 次）；provider 17.574s；tools/pyref 0.826s。

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/oxidns/testdata/pyref -- bash -lc 'cd server && PYREF_MODE=record PYREF_ACCEPT=1 go test -count=1 -timeout 15m ./internal/oxidns -run "TestBoundary|TestC05ReviewSettingsReferences|TestC05ReviewRecordedNormalization|TestStoredLegacySettingsAgainstPython"'
```

退出 0：oxidns 25.105s，按上述追加裁定审查并接受差异。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/repo/files.go internal/repo/legacy.go internal/repo/batch16_regression_test.go)" && PYREF_MODE=check go test -count=1 -timeout 15m ./internal/oxidns && go test -race -count=1 -timeout 15m ./internal/repo ./internal/provider ./internal/api/aggregations'
```

退出 0：gofmt 无差异；实时 pyref check：oxidns 80.574s；race：repo 432.755s、provider 168.251s、api/aggregations 7.281s，全部 PASS。

完整预检命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt sync
ssh sunbroke@192.168.0.32 'source ~/.rt-env && cd ~/rt/.paseo__worktrees__302rx1dg__go-batch-16/server && test -z "$(gofmt -l .)" && bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-batch-16 -p 8'
```

完整预检退出 0；全仓 gofmt -l 无输出、两组 vet 通过，最后输出：

```text
== vet
== full non-race tests (-p 8)
PREFLIGHT PASS
```

本提交包含全部四项修复及审查记录。无待完成项；未 push。新增文件：repo/batch16_regression_test.go（66 行）和本报告。未增加依赖，未修改冻结语料或原测试断言。修复恢复原行为，没有新增功能差异；完整 batch gate 由协调者继续执行。
