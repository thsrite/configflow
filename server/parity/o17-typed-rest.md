# O17 1.6 收尾：剩余 CRUD API 请求体强类型化

分支：`go-o17-typed-rest`（基于 `go@595972901`）。

## 改动摘要

将 batch-44（O17 1.6）之后剩余 4 个 CRUD API 包中的 `model.Fields` / 字典式请求体全部改为强类型结构体，并合并重复的 JSON 对象请求体读取逻辑：

1. **共用请求体读取（`internal/httpx`）**
   - 在 `internal/httpx/bind.go` 新增 `(c *Ctx) BindObject(silent bool) (json.RawMessage, bool, error)` 与泛型 `httpx.DecodeObject[T any](c *Ctx, silent bool) (*T, bool, error)`。
   - 合并并删除 `api/aggregations`、`api/subscriptions`、`api/rulelibrary`、`api/proxygroups` 各自私有的 `readBody` / `requestObject` / `readGroup` / `decodeObject` 副本。

2. **`internal/api/aggregations`**
   - 删除 `aggregationFields struct{ model.Fields }` 与 `stripStats`、`readBody`。
   - 新增 `aggregationRequest`（`jsondecode.Member` 精确成员匹配）与 `decodeAggregation`，逐字段调用 `model.DecodeInputMember` 严格解码为 `model.Aggregation`；创建时在 `health_check` 缺失的情况下直接构造默认 `*model.ProviderHealthCheck`（保留兼容的 `health_check_url` 覆盖）。
   - 类型错误通过 `aggregationTypeError` 构造 `model.MemberTypeError` 并交给 `repo.InputError`，保留原中文报错文案；替换更新直接赋值 `shared.Aggregations[index]`，不再整表回序列化重解码。

3. **`internal/api/subscriptions`**
   - 删除 `subscriptionFields struct{ model.Fields }` 与 `stripDerived`、`readBody`。
   - 新增 `subscriptionRequest` 与 `decodeSubscription`，在 `validateFields` / `validateOptions` 校验通过后按字段严格解码为 `model.Subscription`；创建时在 `health_check` 缺失时填充默认 `*model.ProviderHealthCheck`。
   - 类型错误经 `subscriptionTypeError` → `repo.InputError` 保留原中文报错文案；保留 `proxy map[string]json.RawMessage`（开放 Mihomo 代理协议定义）。

4. **`internal/api/rulelibrary`**
   - 删除 `type record = model.Fields` 以及整套字典辅助函数（`text`/`field`/`put`/`rows`/`child`/`stringField`/`stringsField`/`libraryItems`/`setLibrary`）。
   - 全部接口请求体改为专用 DTO：`libraryRequest`、`draftRequest`（保持严格仅允许 `revision` + `content` 两个字符串成员）、`singleRequest`、`cacheRequest`、`checkContentRequest`、`proxyDomainsRequest`；`GET /api/rule-library` 改为 `libraryListItem`（嵌入 `libraryItemWire` + `HasIPPart`），`GET /draft-content` 改为 `draftRule`。
   - 内部流转、缓存暂存、并发探测、变更判定全部直接使用 `model.RuleLibraryItem`；`declarationChanged`/`routingChanged`/`downloadChanged` 改为结构体字段比较，去掉逐字段 `json.Compact` 字节比对（顺带消除本包的 O17 1.9 痕迹）。

5. **`internal/api/proxygroups`**
   - 删除 `groupFields struct{ model.Fields }`、`fieldsOf`、`groupOf`、`storeGroups`、`merge` 等字典桥接代码。
   - 新增 `groupRequest`（全成员使用 `jsondecode.Member`），在 `Create`/`Mutate` 中按组类型直接清理非所属成员（不论原类型）并补齐健康检查默认值，直接构建 `shared.GroupMembers` 参与候选与筛选校验，通过 `buildGroup` 严格解码为新 `model.ProxyGroup`（无写穿借用快照风险），类型错误经 `groupTypeError` → `repo.InputError` 保持原中文提示。

## 行为变化说明

- **存储结果、HTTP 状态码、中文报错文案**：不变（三组 live Flask 差分全部通过）。
- **成功响应 `data` 回显**：
  - `POST/PUT /api/aggregations`、`POST/PUT /api/subscriptions`、`POST/PUT /api/rule-library`、`POST/PUT /api/proxy-groups` 的响应 `data` 现在直接序列化保存后的强类型记录（`model.Aggregation` / `model.Subscription` / `model.RuleLibraryItem` / `model.ProxyGroup`），按结构体声明顺序输出且不再回显未声明成员（例如请求中附带的自定义未知字段），与 batch-44 中 `api/rules` 和 `api/nodes` 的口径一致。
  - `POST/PUT /api/subscriptions` 的声明字段由原先的 `json.Unmarshal` 宽松解码改为 `model.DecodeInputMember` 严格解码，非法字段类型（如错误类型的 `health_check` 或 `enabled`）现在返回 400 及仓库对应的中文验证提示，而非静默丢弃该字段。
  - 上述变化已同步记录至 `server/parity/behaviour-changes.md` 的「O17 1.6」节。

## 验证（测试服务器 `.64`）

- `git grep -n 'model\.Fields' server/internal/api/{proxygroups,aggregations,subscriptions,rulelibrary}`：无输出（全部清零）。
- `gofmt -l internal/httpx/ internal/api/aggregations/ internal/api/subscriptions/ internal/api/rulelibrary/ internal/api/proxygroups/`：无输出。
- `go vet ./internal/httpx/... ./internal/api/aggregations/... ./internal/api/subscriptions/... ./internal/api/rulelibrary/... ./internal/api/proxygroups/...`：通过。
- `go test -race -count=1`：
  - `internal/httpx`：PASS（61.1s）
  - `internal/api/aggregations` + `difftest`：PASS（5.8s / 1.1s）
  - `internal/api/subscriptions` + `difftest`：PASS（11.5s / 1.1s）
  - `internal/api/rulelibrary` + `difftest`：PASS（31.7s / 1.0s）
  - `internal/api/proxygroups`：PASS（4.3s）
- Live Flask 差分（`CONFIGFLOW_DIFF=1`）：
  - `internal/api/aggregations/difftest`：PASS（57.4s）
  - `internal/api/subscriptions/difftest`：PASS（145.7s）
  - `internal/api/rulelibrary/difftest`：PASS（47.2s）
- Fuzz 短跑（各 30s）：
  - `FuzzBindNativeJSON`（`internal/httpx`）：63,703 execs，PASS
  - `FuzzEditableContent`（`internal/api/rulelibrary`）：328,044 execs，PASS
