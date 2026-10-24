# ConfigFlow 多配置与共享资源

## 先理解三件事

1. **资源共用。** 添加一次订阅后，可在家庭、办公等多份配置中分别选择它。
2. **方案独立。** 策略组、规则去向和生成参数各自保存；改一份方案不会复制或重排另一份。
3. **设备有自己的绑定。** 切换浏览器配置不会改变 Agent，发布前检查卡片上的绑定。

## 当前实现：schema 5

配置分为三层，保存在 `DATA_DIR/configflow.sqlite3`（按 system/shared/每个配置空间的字段分行存储的同一事务文档）中：

| 作用域 | 持久内容 | 页面与影响范围 |
|--------|----------|----------------|
| `system` | 服务地址、全局访问令牌、WebDAV、Agent 清单 | 「系统设置」与「Agent」，影响全部配置 |
| `shared` | 订阅、节点、订阅聚合、规则仓库的资源定义 | 共享资源页面；修改会影响所有引用它的配置 |
| `profiles.<id>` | 策略组与命名代理链、规则排序与目标、Mihomo / Surge / Loon / MosDNS / OxiDNS 参数、域名发现、路由器与设备备注 | 当前配置独立保存，通过稳定 ID 引用共享资源，不复制资源本体 |

Agent 心跳上报的在线时间、服务状态和当前监控指标只保存在服务进程内存里（最近 24 小时的监控历史曲线另由 `MetricsHistory` 写在 `metrics` 目录，不在 `DATA_DIR` 里），读取 Agent 时叠加在注册信息上，以心跳时间较新的一方为准；重启后各 Agent 在下一次心跳前显示为离线。心跳只在上报的版本号或发布基线 `config_version` 变化时写入配置，所以重启后更新检查和版本门槛仍按 Agent 实际运行的版本判断。旧版留下的 `DATA_DIR/runtime.sqlite3` 会在启动时读入一次后删除。

### 使用流程

1. 在「配置空间」创建或克隆配置，再从顶部选择当前配置。选择仅保存在当前标签页，不改变其他用户或 Agent。
2. 在共享资源页面管理资源本体，直接在当前配置的策略组中选择订阅、节点与聚合，不再设置第二份资源白名单。生成时自动推导策略组、代理链和聚合所需依赖；未引用资源不会自动加入配置。
3. 为当前配置建立策略组，在「策略规则」中引用共享规则库并设置目标、启用状态与顺序。同一规则库可在不同配置中分别指向 `DIRECT`、`REJECT` 或不同策略组。
4. 在「策略组」新增 `chain` 类型，前置与落地均选择手动节点或策略组，并为组合命名；订阅和聚合通过策略组使用。前置沿用原组，落地生成独立链组，链名可供规则或其他策略组引用；原节点、原组不变。代理链仅用于 Mihomo，Surge 拒绝启用的代理链。
5. 在「配置生成」预览或下载当前配置；在 Agent 卡片选择绑定配置，推送和拉取始终使用绑定值，不跟随浏览器选择。

复制配置空间会保留策略、规则和模板，并继续引用共享资源；新副本的路由器列表为空，不继承路由器凭据、关联 Agent、自动同步设置和发布归属记录。需要管理设备时，请在新空间重新添加路由器。源空间及其 Agent 绑定不变，已有副本不会自动清理。

被配置或聚合引用的共享资源不能直接删除，接口返回 `409` 与引用者。移除仍被策略组或拨号链使用的资源也会被拒绝，需先调整依赖。共享规则库只存来源；策略目标和编排不随其他配置的修改而变化。

### API 与文件范围

- 共享目录：`/api/subscriptions`、`/api/nodes`、`/api/aggregations`、`/api/rule-library`，忽略无关的配置选择。
- 策略组与代理链统一使用 `/api/profiles/<id>/proxy-groups` 的列表、新建、修改、删除接口。链结构为 `{"id":"via-a","name":"经 A 的 B","type":"chain","chain":{"entry":{"type":"node","id":"a"},"exit":{"type":"node","id":"b"}}}`；两端引用策略组时均使用 `type:"group"`。订阅不使用缓存节点 ID 直接接入链，而是通过策略组来源引用。
- 配置订阅：`/api/config/<id>/mihomo`、`/surge`、`/loon`、`/mosdns`、`/oxidns`；Provider 和规则链接也带配置 ID。
- 独立 API 的选择顺序：显式参数或路径 > 查询参数 `profile` > 查询参数 `profile_id` > `X-ConfigFlow-Profile` > 系统当前默认配置。默认空间初始为 `default`，删除默认并指定接任者后会改变。空或非法 ID 返回 `400`，不存在返回 `404`，不会回退到另一份配置。全局设置、认证、目录和全量备份不受过期的配置选择影响。
- 原始订阅与规则缓存存于 `DATA_DIR/shared/`；生成文件、Provider 与转换规则存于 `DATA_DIR/profiles/<id>/`，互不覆盖。
- 显式订阅/聚合预览与下载可访问共享目录中的资源，无需先被策略组使用；仅为该次请求补齐依赖，不写回配置。Provider 仍在指定配置的拓扑下验证并按配置隔离保存。

### 日常操作与备份入口

| 目的 | 页面操作 | 影响范围 |
| --- | --- | --- |
| 新建方案 | 配置空间 → 新建配置空间 | 新建独立参数与规则编排 |
| 复用一套方案 | 配置空间 → 复制 | 共用原资源；新副本清空路由器接入和同步归属 |
| 导入现成客户端文件 | 配置空间 → 导入客户端配置 | 新建空间，并按支持范围加入共享资源 |
| 备份所有配置 | 系统设置 → 导出全部数据 | 系统、共享资源、全部空间与 Agent |
| 分享排查 | 系统设置 → 脱敏导出 | 隐藏敏感内容，不能用于完整恢复 |
| 恢复一个空间 | 配置空间 → 导入备份到当前 | 引用的共享资源 ID 必须已经存在 |

当前服务的主数据在 `configflow.sqlite3`，派生文件在 `shared/` 和 `profiles/<id>/`。根目录的 `config.json`（若存在）只是迁移前的旧格式副本，不再写入。升级时仍需备份整个实际数据目录（先停服再复制），旧版文件和运行数据也可能需要恢复；服务运行时请用「导出全部数据」。

### 升级、备份与恢复

- 支持原版无版本号单文件配置，以及已发布的 schema 2 多配置目录（`system.json` 与 `profiles/<id>/config.json`）。尚未迁移时，以旧系统索引和各配置文件为准，忽略残留的旧根 `config.json`；已成功写入的 schema 5 则始终优先，不会在重启时被旧目录覆盖。
- 迁移保留配置 ID、名称、生成参数、规则顺序、系统设置和 Agent 绑定。相同 ID、相同定义的资源合并；同 ID 不同定义保留独立资源并重写引用，避免不同配置互相覆盖。Provider／规则库名称冲突时生成独立名称；原始缓存按资源映射迁移，不复用来源不明的共享缓存。
- 旧配置中的原始 `dialer-proxy` 先在各配置内部解析，再合并共享资源。节点身份同时包含所引用订阅的完整定义：即使节点字段与旧订阅 ID 相同，订阅 URL 等来源参数不同，也按不同目标处理。跨配置重名且身份不同的拨号目标会分配独立名称，并同步改写原始拨号引用与节点规则目标；多跳依赖及 JSON/YAML 节点同样保留各自连接语义。相同来源仍可合并；单个配置内真正不唯一的目标仍拒绝迁移。
- 提交新配置前，在 `migrations/<时间戳-标识>/migration/` 保存根文件、旧系统索引及全部旧配置的主文件和 `.bak`。旧文件保留不变；可读 JSON 的索引、引用或模型校验失败时停止迁移，不生成默认模板，也不静默回退到更旧数据。主文件缺失或无法解析时可只读使用有效 `.bak`。
- 首次启动新版本时，迁移结果写入临时数据库，校验通过后原子替换为 `configflow.sqlite3`。`config.json`、`config.json.bak`、`system.json` 和 `profiles/<id>/config.json` 全部原样保留，不再修复或改写；数据库元信息记录迁移来源、快照位置以及当时 `config.json` 的 sha256 和大小。迁移中途失败时旧文件不变，下次启动重试。
- 回退检测：之后每次启动，若 `config.json` 的 sha256 与记录不同，说明回退期间旧版本写过它。当前格式（schema 5）的文件只有其中任一节的 `_revision` 高于迁移时的记录，或配置空间集合不同，才视为在旧版本里有修改；原版单文件格式没有 `_revision`，按规范化后的内容摘要（忽略格式、键顺序和启动时补写的令牌）与记录比较，内容不同即视为修改。视为修改时先把当前数据库备份到 `migrations/<时间戳-标识>/legacy-config/`（含 `configflow.sqlite3` 和当时的 `config.json`），再按全量导入的校验流程导入，并记录新的 sha256，同一份文件不会导入两次。旧版本启动时只是补令牌、重新迁移或写默认配置的改写只记录新 sha256，不导入；无法解析或校验失败的内容会被拒绝一次并记录警告，数据库不变。回退期间旧版本看不到新版本做的修改；再次升级导入后，这些修改仍可从 `legacy-config` 快照找回。
- 全量导入前的快照保存在 `migrations/<时间戳-标识>/import/`，包含导入前的 `configflow.sqlite3`；回退导入的快照见上一条，位于 `legacy-config/`。恢复这些数据库快照前须先停止服务。
- 数据库物理损坏（无法打开、`quick_check` 失败或缺少表/元信息）时，原文件另存为 `configflow.sqlite3.corrupt-<纳秒>`（硬链接或复制，原位置一直保留到新库原子替换，中途崩溃下次启动会重新处理），仅当 `config.json` 本身就是数据来源（迁移自它，或之后从它导入过）且 sha256 与数据库记录一致，或者元信息已无法读取且 `config.json` 不是旧版分目录布局旁残留的旧格式根文件（schema 5 的 `config.json` 即使旁边有 `system.json` 也可用）时，才从它重建，并在日志中以 ERROR 提示迁移之后的修改不在重建结果中、可用的数据库快照位置；否则启动失败并说明原因，损坏文件不做任何改动。数据库被锁、磁盘错误、版本更新或内容校验失败都只报错，不会重建。数据库不再保留 `.bak`，需要回到旧状态时使用 `migrations/` 快照或导出备份。数据库文件缺失、但目录里有 `.corrupt-*` 副本或数据库快照时，启动会从旧配置文件重新迁移，并以 ERROR 列出这些副本。
- 全量导入接受原版单配置、包含 `system` 索引和完整 `profiles` 内容的 schema 2 备份，以及当前格式；不接受只有索引的 `system.json`。开发中间格式（schema 3/4、资源白名单、节点拨号覆盖、旧代理链结构）仍明确拒绝，不静默删除不支持字段。
- 已被有问题的旧升级程序写成 schema 5 的实例不会自动回滚，避免覆盖升级后新增的数据。应先停服并备份完整数据目录，再用升级前快照迁移到副本，核对配置与 Agent 绑定后人工切换；不要直接删除当前根文件或只修改版本号。
- 「系统设置」的全量导出、导入、WebDAV 和重置涵盖共享资源、所有配置、系统设置及 Agent，**不是只操作当前配置**。导入/重置需确认；导入验证失败不会覆盖已有数据。
- 全量导出含恢复所需凭据，需妥善保管；脱敏导出适合排查或分享，不能替代完整备份。配置令牌仍同时授予 MCP 管理权限，不应公开分享订阅链接。
- 「配置空间」的单配置导入/导出仅包含策略组、代理链、规则编排及生成参数；其中的共享资源 ID 必须已存在。跨实例完整迁移请使用全量备份。
- 「配置空间」的「导入客户端配置」（`POST /api/profiles/import-client`，MCP 工具 `import_client_config`）把 Mihomo / Surge / Loon / Shadowrocket 配置文件转换为新配置空间，在同一事务中写入：节点、`proxy-providers`/`policy-path`/`[Remote Proxy]` 订阅、规则集进入共享资源，同 URL 订阅/规则集与名称和定义都相同的节点直接复用，重名时自动改名；策略组、规则编排及该客户端的基础配置（Mihomo 其余顶层字段、Surge/Loon 的非生成段落）写入新配置空间。不支持的节点类型、规则类型（如 `URL-REGEX`、`SUB-RULE`）、`relay`/`subnet` 策略组、mrs 规则集、`dialer-proxy` 以及引用不存在策略的规则会跳过并在结果中提示；`dry_run` 只返回解析摘要。
- 只有一个配置时不能删除默认空间；有其他配置时，页面允许选择接任者再删除。接口使用 `DELETE /api/profiles/<id>?new_default=<接任ID>`。仍绑定 Agent 的配置需要先解除或更换绑定。删除的配置空间会移到 `migrations/deleted-<id>-<标识>/`：派生文件之外还有一份 `deleted-profile.json`（与单配置导出格式相同，权限 0600，含凭据），可用于找回。

---

## 实现参考

[持久化、导入与迁移](../../backend/common/config_repository.py)、[默认空间与请求选择](../../backend/common/profile_context.py)、[配置空间接口](../../backend/routes/profiles.py)、[客户端配置导入](../../backend/utils/config_importer.py)、[浏览器标签页选择](../../frontend/src/profileContext.ts)。

<details>
<summary>历史可行性分析：仅用于了解设计过程，不作为当前操作说明</summary>

以下保留原作者基于 `875d67c8fb32aa66710e918c899380ae16f4520e` 的历史可行性分析，描述的是改造前架构与当时方案；实际使用以本页上方的 schema 5 说明为准，历史方案不代表受支持的迁移格式。

## 1. 历史分析结论

**可行，建议实现。**

但不能只在前端增加“配置下拉框”。当前系统把配置、缓存、生成产物和 Agent 注册全部放在单一全局作用域：

- 单一配置文件：`/data/config.json`
- 单一进程全局对象：`backend.common.config.config_data`
- 单一订阅缓存目录：`/data/subscribes`
- 单一规则/Provider 目录：`/data/rules`、`/data/providers`
- 单一生成产物：`/data/config.yaml`、`/data/config.conf`
- Agent 注册信息也存于同一个 `config_data['agents']`

代码规模约 28,952 行，后端约 8,031 行 Python。约 20 个 Python 文件、450 处引用直接依赖 `config_data`，另有 66 处 `save_config()` 调用。因此多配置属于**中等规模架构改造**，不是小功能。

推荐引入一等公民的 `Profile`（配置空间），把“系统级数据”和“配置级数据”拆开，并让每个 Agent 显式绑定一个 Profile。

## 2. 当前架构与耦合点

### 2.1 配置存储

核心文件：`backend/common/config.py`

当前：

```python
DATA_DIR = os.environ.get('DATA_DIR', '/data')
CONFIG_FILE = os.path.join(DATA_DIR, 'config.json')
config_data = get_default_config()
```

`get_config()` 永远返回同一个全局字典；`save_config()` 永远覆盖同一个 `config.json`。

主要问题：

1. 无法同时加载两套配置；
2. 并发请求切换全局 `config_data` 会串配置；
3. `AgentManager` 持有同一个字典引用；
4. 生成器、路由、MCP 内部调用都默认使用全局配置；
5. 当前写文件不是原子替换，也没有按 Profile 加锁。

另外，`backend/models/config.py` 中的 dataclass 与实际存储模型已经脱节：模型使用 `rules` / `rule_sets`，实际配置使用统一的 `rule_configs`；路由也全部直接操作 `dict/list`。因此不能只在 dataclass 上增加 `profile_id`，需要以 ConfigRepository 为真实数据入口。

### 2.2 缓存和生成产物

- `backend/utils/subscription_cache.py` 固定使用 `/data/subscribes/<sub_id>.json`
- `backend/common/config.py` 固定使用 `/data/providers`
- `backend/utils/rule_utils.py` 使用全局规则目录
- `backend/routes/generate.py` 固定保存 `/data/config.yaml` 和 `/data/config.conf`

即使只给 `config.json` 增加多配置数组，缓存和生成文件仍会互相覆盖。

### 2.3 REST API

现有 API 都没有配置上下文：

```text
/api/subscriptions
/api/rules
/api/proxy-groups
/api/generate/mihomo
/api/config/mihomo
/api/agents/<id>/push-config
```

所有路由内部直接 `get_config()` 或直接引用 `config_data`。

### 2.4 Agent 推送

核心：`backend/routes/agents.py::push_config_to_agent()`

当前根据 Agent 的 `service_type` 生成配置，但生成源永远是全局 `config_data`：

```python
generate_mihomo_config(config_data, ...)
generate_mosdns_config(config_data, ...)
```

因此目前不可能安全地让 `.28` 使用 `100.127.0.0/17`，同时让 `.29` 使用 `100.126.0.0/17` 并都由同一 ConfigFlow 管理。

### 2.5 MCP

MCP 工具通过 `backend/mcp_server/invoker.py` 进程内调用现有 REST API，这是好的设计，可继续复用。

问题是 MCP 当前无状态；工具没有 `profile_id` 参数，因此所有调用都落到唯一全局配置。

### 2.6 前端

前端没有集中式配置状态库，每个页面直接调用现有 API。`frontend/src/api/index.ts` 的 Axios 拦截器当前只注入 Authorization。

这反而使改造相对容易：可以在 Axios 请求拦截器中统一注入 Profile 上下文，并通过顶栏选择器切换。

## 3. 推荐领域模型

### 3.1 系统级数据

不属于任何 Profile：

```json
{
  "schema_version": 2,
  "active_profile_id": "default",
  "profiles": [
    {
      "id": "main-28",
      "name": "主代理 .28",
      "description": "Mihomo/MosDNS 主实例",
      "created_at": "...",
      "updated_at": "..."
    }
  ],
  "agents": [],
  "system_config": {
    "server_domain": "...",
    "github_proxy_domain": {},
    "config_token": "..."
  },
  "backup": {}
}
```

建议全局保存：

- Profile 索引；
- Agent 注册信息和最近上报的版本号（心跳和监控元数据只在内存中）；
- 登录/认证相关系统设置；
- ConfigFlow 服务地址；
- 全局备份配置。

### 3.2 Profile 级数据

每个 Profile 独立保存：

```json
{
  "subscriptions": [],
  "nodes": [],
  "subscription_aggregations": [],
  "rule_configs": [],
  "rule_library": [],
  "proxy_groups": [],
  "mihomo": {},
  "mosdns": {},
  "surge": {}
}
```

### 3.3 Agent 绑定

为 Agent 增加：

```json
{
  "profile_id": "main-28",
  "service_type": "mihomo"
}
```

同一个 Profile 可绑定多个 Agent；一个 Agent 默认只绑定一个 Profile。

你的实际场景：

```text
main-28
  ├─ ros-mihomo
  └─ ros-mosdns

backup-29
  ├─ ros-mihomo29
  └─ ros-mosdns29

nas-aio
  ├─ aio-mihomo
  └─ aio-mosdns
```

`.28/.29` 可分别配置：

```text
main-28 fake-IP   = 100.127.0.0/17
backup-29 fake-IP = 100.126.0.0/17
```

这样 ConfigFlow 才能安全推送，不会把 `.28` 配置覆盖到 `.29`。

## 4. 推荐磁盘布局

```text
/data/
├─ system.json
├─ profiles/
│  ├─ main-28/
│  │  ├─ config.json
│  │  ├─ subscribes/
│  │  ├─ providers/
│  │  ├─ rules/
│  │  └─ generated/
│  │     ├─ config.yaml
│  │     └─ config.conf
│  ├─ backup-29/
│  │  └─ ...
│  └─ nas-aio/
│     └─ ...
├─ logs/
└─ app.log
```

不要让不同 Profile 共享：

- subscription cache；
- provider 文件；
- 本地规则缓存；
- 生成文件；
- MosDNS 自定义文件。

即使两个 Profile 的 subscription ID 相同，也不能覆盖彼此。

## 5. 后端实现方案

### 5.1 引入 ConfigRepository

新增：

```text
backend/common/config_repository.py
backend/common/profile_context.py
```

建议接口：

```python
class ConfigRepository:
    def list_profiles(self): ...
    def create_profile(self, metadata, clone_from=None): ...
    def get_profile(self, profile_id): ...
    def save_profile(self, profile_id, data): ...
    def delete_profile(self, profile_id): ...
    def export_profile(self, profile_id): ...
    def import_profile(self, profile_id, data): ...
    def profile_dir(self, profile_id): ...
```

写入要求：

1. 每个 Profile 独立锁；
2. 写入临时文件；
3. `fsync` 后 `os.replace()` 原子替换；
4. 保留最近一次有效副本；
5. 校验 profile ID，防止路径穿越。

### 5.2 请求级 ProfileContext

建议用 Flask `g` 或 `contextvars.ContextVar`，不要在请求开始时替换全局 `config_data`。

解析优先级：

```text
显式 URL profile_id
→ X-ConfigFlow-Profile 请求头
→ query 参数 profile
→ system.active_profile_id
→ default
```

推荐显式 URL 用于外部配置和危险操作；Header 用于前端普通 CRUD。

Profile 上下文最好同时进入请求日志，便于审计一次规则修改、生成或推送究竟作用于哪套配置。

### 5.3 兼容现有路由

生产级推荐两层兼容：

```text
旧：/api/rules
新：/api/profiles/<profile_id>/rules
```

旧路由解析到 `active_profile_id`，保证升级后旧客户端继续工作。

外部订阅 URL：

```text
/api/config/<profile_id>/mihomo?token=...
/api/config/<profile_id>/surge?token=...
/api/config/<profile_id>/mosdns?token=...
```

旧 URL：

```text
/api/config/mihomo
```

继续指向默认 Profile。

### 5.4 降低改造面

短期兼容可以保留 `get_config()`，改为：

```python
def get_config(profile_id=None):
    return repository.get_profile(resolve_profile_id(profile_id))
```

但应逐步消除模块级 `from ... import config_data`。全局代理对象虽然能减少代码改动，但在并发 Web/MCP/后台任务中容易串 Profile，不建议作为最终方案。

所有生成器本身已经接收 `config_data` 参数，因此生成器核心无需重写；只需在调用点传入正确 Profile 数据。

## 6. REST API 设计

### Profile 管理

```text
GET    /api/profiles
POST   /api/profiles
GET    /api/profiles/<id>
PUT    /api/profiles/<id>
DELETE /api/profiles/<id>
POST   /api/profiles/<id>/clone
POST   /api/profiles/<id>/activate
GET    /api/profiles/<id>/export
POST   /api/profiles/<id>/import
```

删除限制：

- 默认 Profile 不可删除；
- 被 Agent 绑定的 Profile 不可直接删除；
- 删除前必须确认生成产物和缓存范围；
- 不允许静默把 Agent 迁移到默认 Profile。

### Profile 内资源

推荐显式路径：

```text
/api/profiles/<id>/subscriptions
/api/profiles/<id>/nodes
/api/profiles/<id>/rules
/api/profiles/<id>/rule-library
/api/profiles/<id>/proxy-groups
/api/profiles/<id>/generate/mihomo
```

为了减少一次性改动，也可先让旧资源 API 接受：

```text
X-ConfigFlow-Profile: main-28
```

然后分阶段迁移到显式路径。

## 7. Agent 推送设计

修改 `push_config_to_agent()`：

```python
profile_id = agent['profile_id']
config_data = repository.get_profile(profile_id)
```

推送前响应/日志必须包含：

```text
Agent 名称
Agent 地址
Profile 名称和 ID
配置类型
配置版本/hash
是否重启
```

建议 Agent 增加：

```json
{
  "profile_id": "backup-29",
  "config_revision": "sha256:..."
}
```

推送安全检查：

- 未绑定 Profile 时禁止推送；
- 检测 fake-IP CIDR 与目标预期不一致时警告/阻止；
- 推送前 preview；
- 配置 hash 未变化时跳过；
- 保留 Agent 端上一版配置以回滚。

`backend/common/agent_manager.py` 当前是持有全局 `config_data` 引用的单例，必须拆成“全局 Agent 注册仓库 + 显式 Profile 配置仓库”，不能为每个请求重新绑定一个可变全局字典。

同一个 Agent 的配置更新应加互斥锁或任务队列：当 Web、MCP 或后台任务同时向同一 Agent 推送时，只允许一个更新任务执行；后续任务按配置 revision 去重或排队。Go Agent 回报的 `config_version` 应与目标 Profile revision 对应，服务端在推送完成后进行确认。

## 8. MCP 设计

MCP 目前无状态，因此不能依赖“上一次选择了哪个配置”。

### 新增工具

```text
list_profiles
get_profile
manage_profile
clone_profile
bind_agent_profile
```

### 现有工具参数

所有配置相关工具增加可选：

```json
{
  "profile_id": "main-28"
}
```

兼容逻辑：

- 未传 `profile_id` → 默认 Profile；
- 涉及写入或 Agent 推送时，结果中必须回显 Profile ID；
- 危险工具不可依赖一个全局“当前 Profile”会话状态。

MCP 内部调用：

```python
call_api(..., headers={'X-ConfigFlow-Profile': profile_id})
```

需扩展 `backend/mcp_server/invoker.py::call_api()` 支持额外 headers 或显式 Profile 路径。

## 9. 前端实现

### 9.1 Profile Store

新增轻量 store（可用 Pinia，或先用 `provide/inject`）：

```text
frontend/src/stores/profile.ts
frontend/src/components/ProfileSwitcher.vue
frontend/src/views/Profiles.vue
```

状态：

```ts
activeProfileId
profiles
loading
switchProfile(id)
refreshProfiles()
```

### 9.2 Axios 注入

`frontend/src/api/index.ts` 请求拦截器中增加：

```ts
config.headers['X-ConfigFlow-Profile'] = activeProfileId
```

配置生成和订阅 URL 使用显式 profile path，不能只靠 Header。

### 9.3 页面刷新

Profile 切换后：

- 清空当前页面缓存；
- 取消旧 Profile 未完成请求；
- 使用 `router-view :key="activeProfileId"` 重新挂载页面；
- 页面标题/危险操作对话框显示 Profile 名称；
- 在 Agent 页面显示绑定 Profile。

Profile 选择不应只保存在全局 `localStorage`。若用户同时打开两个标签页分别编辑 `.28` 和 `.29`，共享 localStorage 会让一个标签页的切换影响另一个标签页。推荐：

- Profile ID 放入 URL（例如 `/profiles/main-28/rules`），或
- 使用 `sessionStorage` 保存每个标签页的活动 Profile；
- Axios 请求仍显式携带 Profile ID；
- 服务器不依赖浏览器“当前配置”状态。

### 9.4 UX

顶栏显示：

```text
当前配置：[主代理 .28 ▼]
```

切换时明确提示未保存编辑。克隆 Profile 是重要入口：

```text
从“主代理 .28”克隆 → “备用代理 .29”
```

然后只调整 fake-IP、DNS、节点策略和 Agent 绑定。

## 10. 迁移方案

### 首次启动迁移

检测到旧 `/data/config.json` 且不存在 `/data/system.json`：

1. 完整复制为 `/data/migrations/<timestamp>/config.json`；
2. 建立 `default` Profile；
3. 把配置级字段迁到 `/data/profiles/default/config.json`；
4. 把 `agents`、`system_config`、`backup` 移入 `/data/system.json`；
5. 把旧缓存目录移动到 default Profile；
6. 写入 `schema_version=2`；
7. 验证数量和引用一致后再提交迁移标记。

迁移必须幂等；中途失败时继续使用旧配置，不能写出空配置。

### 兼容策略

- 默认 Profile ID 固定为 `default`；
- 老 API/订阅链接默认指向 `default`；
- 老 MCP 调用不传 profile 时仍工作；
- 配置导入默认导入到当前 Profile，不覆盖系统/Agent 数据；
- 增加“导出单个 Profile”和“导出完整系统”两种模式。

## 11. 两种实现档位

### 方案 A：多配置存储 + 全局活动配置

特点：一次只能有一个 active Profile，所有 Agent 仍使用活动配置。

优点：改动小。
缺点：不能安全同时管理 `.28/.29`；切换期间并发请求和 Agent 推送风险高。

**不建议作为最终实现。** 最多作为短期 MVP UI。

### 方案 B：请求级 Profile + Agent 显式绑定

特点：多个 Profile 可并存、并发生成、分别推送。

优点：真正解决主备、多设备、多环境。
缺点：需要改造配置仓库、缓存路径、REST、Agent、MCP、前端和迁移。

**推荐方案。**

## 12. 实施阶段

### Phase 0：测试与基线（1–2 天）

- 为配置加载/保存/导入补测试；
- 为生成器建立固定快照测试；
- 为 Agent 推送建立 fake Agent 测试；
- 记录现有 REST/MCP 兼容行为。

### Phase 1：ConfigRepository + 迁移（2–3 天）

- system/profile 存储；
- 原子写入和锁；
- 旧配置迁移；
- Profile CRUD；
- 缓存目录隔离。

### Phase 2：请求上下文和 REST（2–3 天）

- ProfileContext；
- 现有 API 兼容；
- 显式 Profile API；
- 配置生成/订阅 URL 多 Profile 化。

### Phase 3：前端（2–3 天）

- ProfileSwitcher；
- Profile 管理页；
- Axios 上下文；
- 切换刷新和危险操作提示。

### Phase 4：Agent + MCP（2–3 天）

- Agent profile_id；
- 按绑定配置推送；
- MCP profile_id 参数；
- MCP Profile 工具；
- 配置 hash 和误推送防护。

### Phase 5：回归和部署（2 天）

- default 兼容；
- `.28/.29` 并行验证；
- 导入导出、备份恢复；
- 并发请求测试；
- 回滚测试。

完整生产级实现预计约 **9–14 个开发日**。只做“可保存多份并手动切换”的 MVP 约 **3–5 天**，但不满足 `.28/.29` 同时管理的核心需求。

## 13. 关键风险

1. **全局字典并发串配置**：必须使用请求级上下文/显式传参；
2. **缓存文件串 Profile**：所有派生文件必须命名空间隔离；
3. **Agent 误推送**：必须绑定 Profile，并在推送确认中展示目标；
4. **配置令牌权限**：多 Profile URL 必须明确授权模型；
5. **备份语义变化**：区分单 Profile 与全系统备份；
6. **ID 冲突**：克隆时可保留内部 ID（独立命名空间）或统一重映射，必须保持引用完整；
7. **配置迁移写空**：迁移应先写新目录、验证后原子切换；
8. **后台任务上下文**：订阅刷新、Agent 心跳、MCP 调用不能依赖可变全局 active Profile；
9. **生成文件 URL**：规则和 Provider URL 必须携带 Profile；
10. **删除 Profile**：有 Agent 绑定或活动订阅链接时必须阻止。

## 14. 推荐落地路径

推荐先从现有配置克隆三套 Profile：

```text
main-28    → ros-mihomo + ros-mosdns
backup-29  → ros-mihomo29 + ros-mosdns29
nas-aio    → aio-mihomo + aio-mosdns
```

第一版完成后至少验收：

- 三个 Profile 的规则/订阅/策略组独立；
- 同时 preview 三套配置不串数据；
- `.28/.29` fake-IP 分别保持 `100.127/17` 与 `100.126/17`；
- Agent 推送严格按绑定 Profile；
- MCP 读写指定 Profile；
- 老 `/api/config/mihomo` 和老 MCP 调用仍指向 default；
- 删除/切换 Profile 不影响其他配置。

## 15. 最终建议

采用 **方案 B：请求级 Profile + Agent 显式绑定**。

不要通过“切换全局 config_data 后复用全部 API”的方式实现，因为 Web、MCP、Agent 心跳和后台任务可以并发，会有把 `.28` 配置推给 `.29` 的真实风险。

最佳技术路线是：

```text
ConfigRepository
+ ProfileContext
+ profile-scoped storage
+ explicit Agent binding
+ MCP profile_id
+ frontend ProfileSwitcher
+ backward-compatible default profile
```

生成器已经是接收 `config_data` 的纯函数风格，核心转换逻辑可复用；真正需要重构的是配置取得、持久化、缓存命名空间和调用上下文。整体工程可控，具备较高实施可行性。

</details>
