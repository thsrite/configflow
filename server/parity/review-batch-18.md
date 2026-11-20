# Batch-18 集成提交复核（HR，fresh Opus，2026-11-20）

范围：只复核 `8b8f562b3`（go(n8-11): batch-18 — unify linuxgateway snapshot, deduplicate auth validation, record W6 L2）。
W5、W6 及其复核不在范围内。

## 1. repo Scope 新增 typed `repo.Config` + `docs` 缓存 —— 撤回，改为最小方案

**判断：不必要。** 要求是 linuxgateway provider 的鉴权和渲染使用同一配置快照。provider 在处理函数里做一次
typed 读取，`model.Config` 同时提供 `SystemConfig`/`Backup` 给 `auth.ValidateSystemTokenOrJWT`，也直接交给
`gateway.NewConfig`。两者天然出自同一 generation，repo 核心不需要任何改动。

核对过“同一请求内是否有其他地方先读配置、必须共享 generation”：

- 生产代码的 BeforeRequest 只有 `auth.Gate`（只校验 JWT，不读配置）和 profile 解析（`ResolveProfileID`，只缓存 id）。
- provider 之前的请求链路没有 `repo.GetConfig` 调用；`repo.Config` 在全仓库只有这一个调用方。
- 因此旧 `repo.GetConfig` 请求内缓存的语义（先读者钉住 generation）对 provider 没有可观察差别。只有测试用
  BeforeRequest 钩子人为制造了“先 GetConfig 再轮换”的场景。

撤回的 Scope 新增本身还有问题，留作记录：

- `docs` 只在 `ResetConfigContext`、`bind`、`UpdateConfig`、`UpdateConfigTransaction` 失效；`SaveConfig`、
  `UpdateSharedConfigs`、`CommitConfig` 等写入口不失效，同一请求写后再读会拿到旧 generation。
- 新测试只覆盖 Reset 和 UpdateConfig 两个失效点。
- 它给 W9 计划要删除的 jsonx Scope 缓存加了一个 typed 平行缓存，W9 计划里没有交代它的去留。

**处理：**

- `repo/scope.go`、`repo/scope_edits.go`、`repo/legacy.go`（`compatConfigWithBaseline` 签名）、`repo/scope_test.go`
  恢复到 `8b8f562b3^`，repo 核心没有净改动，W9 计划不用补。
- `api/linuxgateway/routes.go` 的 provider：`ResolveProfileID` → `GetRepository` → `repository.Config` 一次读取，
  鉴权和 `gateway.NewConfig` 共用这一个结果，并加注释说明。
- `api/config/live_test.go::TestLiveAndLinuxGatewayAuthUseRequestSnapshotOnTokenRotation` 按路由写期望：
  - live 路由仍用请求内 compat 视图（Python `g` 语义），先前 GetConfig 钉住的轮换前 token 通过，轮换后 token 返回 401。
  - provider 在处理函数内只读一次，看到的是轮换后的 token；轮换前 token 返回 401。
  - 后续请求的断言不变。
- 负控：把 provider 临时恢复成 W5 的混合写法（鉴权用请求内 GetConfig，渲染重新做 typed 读取），linuxgateway 两个子用例都失败（401≠200、200≠401），
  说明测试能抓到鉴权和渲染用了不同 generation 的情况。

## 2. auth 去重 —— 无误

- 保留的 `auth/token_validation.go::ValidateSystemTokenOrJWT` 是 W3 L1 收紧版：
  - 依据存储的原始 JSON 类型判断，只有字符串算凭据，null 或缺失算未配置，其他类型一律 fail closed。
  - retired token 逐个解码，坏邻居不影响识别。
  - 内部能力先拒绝。
  - nil system 时先 `lifecycle.Detach`，再检查 `ctx.Err()`。
- 删掉的 W5 `auth/system.go` 会把 `0.0`/`[]`/`{}` 当作未配置并放行，和 behaviour-changes「Opus 复核 L1 后协调者裁定收紧
  （2026-11-20）」冲突，删除正确。
- `system_test.go` 把 `0.0`/`{}`/`[]` 的期望改为 401，与上述裁定一致（数字、数组、对象一律 401）。
- 同快照语义：live 与 provider 都传入非 nil `*model.System`，不走 nil 分支的独立读取。

## 3. 其余改动 —— 无误

- `legacy_gate_allow.txt` 删除 `internal/api/clientdevices/handlers.go:configflow/internal/jsonx`：该文件已不导入 jsonx
  （只剩 `handlers_test.go`，那一行仍在 allowlist）；门禁测试通过。
- behaviour-changes 的 W6 L2 条目与上一条（错型 `nodes` 不进入 `model.Config.Nodes`/Mihomo 输出）互补，不重复也不矛盾。

## 验证（.64）

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'test -z "$(gofmt -l .)" && go vet ./internal/repo/... ./internal/auth/... ./internal/api/linuxgateway/... ./internal/api/config/... && go test -race -count=1 ./internal/repo/... ./internal/auth/... ./internal/api/linuxgateway/... ./internal/api/config/... && go test -count=1 -run "TestLegacyFacadeBoundary|TestLegacyGateNegativeControls" ./internal/lifecycle && CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=20m ./internal/api/config/difftest ./internal/api/linuxgateway/difftest'
```

```text
ok  	configflow/internal/repo	298.182s
ok  	configflow/internal/auth	4.935s
ok  	configflow/internal/api/linuxgateway/difftest	1.077s
ok  	configflow/internal/api/config	15.684s
ok  	configflow/internal/api/config/difftest	1.090s
ok  	configflow/internal/lifecycle	0.346s
ok  	configflow/internal/api/config/difftest	183.537s   (CONFIGFLOW_DIFF=1)
ok  	configflow/internal/api/linuxgateway/difftest	33.964s (CONFIGFLOW_DIFF=1)
```
