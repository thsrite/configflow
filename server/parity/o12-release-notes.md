# O12：Go 切换发布说明（终稿草案）

本文把 Go 后端相对 Python 后端所有**用户能感知**的差异整理成发布说明草案，供 O14 切换计划和公开升级说明复用。资料快照：2026-11-22，`go@65d03d17c`（N8-11 完成、O13 完整性审计之后）。本文只汇总已有裁定，不新增产品决定；O14 演练、实验网 L2–L6 和 #40 发布镜像均未完成。

## 读法

- 核对范围：[behaviour-changes.md](behaviour-changes.md) 全部条目（含 2026-11-19 之后新增的 N8-9 subs/substore、N8-9 routers、N8-10、N8-11 W1–W9）、`server/` 下所有 `review-*.md`、`parity/progress/*.md`、`PROGRESS-*.md`、各 `*-HANDOFF.md`、[O14-CHECKLIST.md](O14-CHECKLIST.md)、[o13-completeness.md](o13-completeness.md) 和总交接 [HANDOFF.md](../HANDOFF.md) 顶部。同一决定只登记一次，采用较新的明确裁定。
- 分类：**第 0 节**是需要维护者确认的事项（先看）；**第 1 节**是升级时一次性影响；**第 2 节**是行为变化（2.1 受影响部署需要处理，2.2 一般变化）；**第 3 节**是不再支持；**第 4 节**是升级步骤与停损要点草案（供 O14）。附录 A 是第 1 节和 2.1 各条的详细做法，附录 C 只计数纯内部差异。
- 每条写明：对用户的影响、是否需要用户操作、依据。2.2 节表格各条**默认不需要用户操作**（遇到 400/409/502 时按中文提示修正该次输入或损坏记录即可），需要操作的条目在行内写明。
- 已修复的 Go 移植错误不是差异；测试比较器里的时间/随机 ID 归一化不是产品差异；未做实验验证的项目不等于功能差异。
- 下文"来源"均为仓库相对链接，节名或问题号用于定位。

## 0）需要维护者确认（先看）

### 0.1 原草稿 13 条的现状

| 编号 | 事项（一句话） | 现状 | 建议 |
|---|---|---|---|
| M01 | 原生 regex/IDNA/Unicode 少见输入（`\N{name}`、İ/ı/ſ 大小写、IDNA2003 专有名、带 scope 的网段、IPv4-mapped 文本）可能与 Python 不同 | **仍需确认**。N8-10 已把 Agent 主机名改为 x/net/idna 校验，其余残余未变 | 接受为原生行为，不做预检工具；发布说明只写"少数特殊正则/国际化域名写法可能需要调整"，不宣传"正则全兼容" |
| M02 | CORS 只对同源 Origin 发许可头，独立域名前端/第三方网页调用 API 的部署数未知 | **仍需确认**（只差发布措辞） | 接受；公开说明加一句"请使用同源面板或同源反代路径"，不写成"跨域请求一律 403" |
| M03 | 数值文本、布尔严格化对手工库/导入文件的影响未知 | **部分解决**：W7 把存储开关统一回 Python 的 JSON 真值（B85），影响面明显缩小；W1 `_revision` 字面量（B80）、W3 config_token 非字符串 fail closed（B81）已裁定 | 不做批量预检；错误都有中文提示和字段路径。仍可达的是手工/损坏数据，按提示逐项修正 |
| M04 | 镜像合同：Go 取代 Nginx、TLS 用系统 CA（旧 requests 用 certifi、`REQUESTS_CA_BUNDLE`）、tzdata、嵌入资源 | **仍需确认**，依赖 #40 发布镜像（O13 确认尚未完成） | #40 镜像装 `ca-certificates` 和 `tzdata`；私有 CA 改用 Go 原生的 `SSL_CERT_FILE`/`SSL_CERT_DIR`，在说明里写明 `REQUESTS_CA_BUNDLE` 不再生效；Web 端口切换纳入 L6 演练 |
| M05 | 下载路由条件请求/Range/日期解析与 Werkzeug 的早期差异 | **大部分解决**：N8-10 api/agents 裁定日期用 `net/http.ParseTime`（无效日期忽略条件，B77）；L1 D7 不可满足 Range→416（B07）。Latin-1 空白、304 头集合等早期细节随 pystr 删除改为原生 HTTP 语法 | 关闭：下载方是安装脚本/curl/浏览器，标准请求不受影响；不再作为发布阻塞项 |
| M06 | 旧 Agent HTTP/Sub-Store/rulefetch 的 `.netrc`、默认头、redirect、编码猜测等差异；Sub-Store 64 MiB 是否覆盖 | **大部分解决**：N8-9 subs 已合并，Sub-Store/订阅响应 64 MiB 限制已补（B63）；N8-10 改原生 MIME/IANA charset；W4 rulefetch 原生化 | 关闭剩余早期接受项（不读 `.netrc`、无 BOM 的 UTF-16/32 不猜测、redirect 次数按 Go 默认）；附录 C 计数，不进公开说明 |
| M07 | HA 主备一台永久离线时 journal 卡在 unknown；维护者 2026-11-18 决定"离线 ≥10 分钟后提供强制停用" | **仍需确认**：设计已定，代码**尚未实现**（设计文档写明"切换后实现"）；现有只有停用时离线成员的强制清除提示 | 首发说明列为已知限制并给人工处理步骤（停用→按提示强制清除→在设备上删除 keepalived 中的 ConfigFlow 配置）；切换后尽快实现。确认是否接受带此限制发布 |
| M08 | RouterOS/iKuai 归属与补偿的真实固件行为（ID 复用、console 结果、netmask 后缀、PD 晚写、断链/关机遗留） | **仍需确认**：L2–L6 均未运行 | 维护者自己切换前至少跑 L6 演练；公开发布前跑 L3/L5。现场失败的人工核对步骤写进 O14 |
| M09 | 历史中间状态的 503 占位（任务 slot、artifact、A21/C36 owner、A14 共享 commit skip） | **已解决**：O13 逐个核对 `internal/api` 全部 503 都是有条件分支，artifact 是真实实现，任务实时 200 | 关闭 |
| M10 | 早期手工/损坏数据边界（SQLite 超 int64、unlink 失败、metrics 目录、NaN 排序等） | **大部分解决**：N8-10/N8-11 的原生 owner 统一 fail closed，并给出现有恢复入口（B74、B76、B78） | 关闭；恢复方法已在 `doc/manual/agents.md`、`doc/manual/oxidns-panel.md`，O14 引用 |
| M11 | A11 RulesFingerprint 依赖构建版本；都以 dev 构建会共享标识 | **仍需确认**：`RulesFingerprint` 读 `version.Version` 和 `vcs.revision`；Docker 构建目录没有 `.git` 时只有版本号 | #40 构建时注入唯一版本（`-ldflags -X` 或保留 VCS 信息），每次发布都改版本号；Agent 二进制和镜像同一提交构建 |
| M12 | MCP 多步工具可能部分完成后返回 -32603 | **仍需确认**（只差说明） | 公开说明加一句"MCP 报 -32603 后先查询当前状态再重试"；不改工具事务设计 |
| M13 | N8-1 只修响应 nonfinite；Agent 新上报是否仍可能写入 nonfinite | **已解决**：N8-10 合并后心跳数值越界 400、指标只接受有限值、monitor 回包按 N8-1 逐值转 null；W3 资源请求 NaN→400 | 关闭；说明里写"旧文件中的 NaN 按缺失显示"即可 |

### 0.2 新增待确认项

| 编号 | 事项（一句话） | 依据 | 建议 |
|---|---|---|---|
| M14 | autoupdate 镜像 tag（容器启动时拉源码重建）在 Go 版本保留还是下线 | [o13-completeness.md 需要维护者决定 1](o13-completeness.md) | 下线：Go 版要带完整工具链，镜像大、启动慢；普通镜像 tag + Agent 在线升级已覆盖。第 3 节 D06 按此写 |
| M15 | `GET /api/agents/<id>/logs` 等对不存在的 Agent 返回 500「Agent not found」（Python 原样保留） | [o13-completeness.md 需要维护者决定 2](o13-completeness.md) | 按"Python bug 在 Go 修正"改 404（前端不依赖 500，规模 S），切换后小任务，不阻塞发布 |
| M16 | EasyTier 生成对 Agent 上报的**非空但无法识别**的内核版本直接拒绝（「Agent 上报的内核版本无法识别：…，请升级或重启 Agent」）。`subs.CoreVersion` 只认 `vX.Y.Z` 形式，Mihomo alpha 内核若上报 `alpha-<hash>` 会被拒 | [behaviour-changes：N8-9 subs 跨包调用边界收口、Agent 未知版本生成边界](behaviour-changes.md) | **发布前确认**：在实验网核对 alpha 内核实际上报的 coreVersion。若是 `alpha-*`，建议把无法识别的版本当"未知"处理（与空版本同路径并告警），而不是拒绝生成；否则在说明中写明 EasyTier 出站需正式版内核 |
| M17 | 不回退 Python：原 A20（退回 Python 前清理 null 历史点）是否整体删除 | 本次简报"Python 已退休，不回滚 Python"；[O14 补充](progress/o14-switch.md) | 删除常规文档中的回退步骤，A20 只留在附录作历史记录；停损按第 4.4 节"修复前进 + 用切换时备份重启 Go" |
| M18 | domain-discovery 流量文件整份损坏（空文件、截断、NaN、顶层非对象）时，该 Agent 上报一直 500 且不覆盖（Python 会当空文件覆盖） | [behaviour-changes：N8-10 HR L2 协调裁定](behaviour-changes.md) | 接受（已裁定）；公开说明在"损坏记录恢复"里给出 `POST /api/agents/<id>/domain-discovery/clear`。确认是否需要在 UI 上给入口（目前只有 API） |
| M19 | HTTP 响应中非整数浮点 ≥1e6 改用指数形式（`1.2345675e+06`），数值相等 | [behaviour-changes：N8-10 metrics HR 复核补登 R2/R3](behaviour-changes.md) | 接受；只影响直接解析 JSON 文本的第三方脚本，前端无影响。公开说明不单列，B78 已登记 |

## 1）升级时一次性影响

切换后只发生一次；完成一次相应操作后，Go 内的新状态保持稳定。详细做法见附录 A 对应编号。

| 编号 | 事项 | 对用户的影响 | 需要操作 | 依据 / 详情 |
|---|---|---|---|---|
| U01 | 所有 Agent 显示一次 `changed`，需要全量推送一次 | Go 原生 YAML/tar/gzip 与 D2 私有 provider 路径改变配置字节和包摘要；不推送则设备继续运行旧配置，面板一直待同步 | **是**：逐台全量发布，确认核心加载成功、content sync 为 `synced`；不要用改指纹或清状态消掉 changed | 附录 A04；[O14-CHECKLIST](O14-CHECKLIST.md)、[差异表 N7-4、A11、D2](behaviour-changes.md) |
| U02 | 远程规则缓存摘要改为原生编码，首次可写读取时迁入新文件名 | 旧缓存仍是离线兜底；可写读取会校验后迁入新名、不延长刷新期；不预热且上游离线时生成可能缺规则 | **是**：保留旧缓存，在首次全量推送前通过缓存刷新入口预热并核对；不手工改文件名 | 附录 A03；[差异表 N8-9 rules/D2、batch-8](behaviour-changes.md) |
| U03 | Python 时期已 ready 未激活的 OxiDNS 候选失效 | 激活时提示重新推送并停止，不发设备请求 | **是**（仅有此类候选时）：重新推送、校验后再激活 | 附录 A05；[差异表 A11](behaviour-changes.md) |
| U04 | IPv6 readiness/验收报告重置 | 旧报告保留并标为陈旧，不能授权当前配置 | **是**（使用 IPv6 功能时）：重新检查、按需 prepare、重做一次验收 | 附录 A06；[差异表 R11、A23、batch-7 D2](behaviour-changes.md) |
| U05 | 切换前生成的固定 IP 预览失效 | 提交旧预览返回 409「固定 IP 预览无效或已过期，请重新预览确认。」，发生在写设备前；已生效绑定不受影响 | **是**（有未确认的预览时）：重新载入来源设备并重新预览 | 附录 A07；[差异表 R17、O9-M3](behaviour-changes.md) |
| U06 | DNS 跟随的旧绑定证明失效 | 不重选会阻断跟随写入 | **是**（使用 DNS 跟随时）：重新选择绑定并保存 | 附录 A08；[差异表 A17](behaviour-changes.md) |
| U07 | 路由器自动规则同步因旧 controls 摘要不匹配而暂停 | 该设备自动同步保持 paused | **是**（出现暂停时）：手动"预览发布"一次建立新基准 | 附录 A09；[差异表 R14](behaviour-changes.md) |
| U08 | 已忽略通知中仍存在的问题重新出现一次；未读 router-check 通知可能标为陈旧 | 只影响通知列表；不触发同步/发布，不新增待办 | **是**（轻量）：查看后再忽略一次；Go 内再次忽略后不会反复出现 | 附录 A10；[差异表 N8-7 second batch、batch-7 D2](behaviour-changes.md)、[O14 补充](progress/o14-switch.md) |
| U09 | 切换前打开的编辑页和临时预览（规则库草稿、路由策略工作区、手工路由、网关草稿预览） | 首次保存可能 409 或预览过期 | **是**（轻量）：刷新页面重新读取后再保存，不绕过 revision | 附录 A11；[差异表 D2/C33、A20、R15](behaviour-changes.md) |
| U10 | 正在进行中的设置向导 | 旧会话没有 digest_version，继续提交时提示配置已被其他页面修改 | **是**（有进行中向导时）：放弃当前向导重新开始；已有设备/配置依赖按放弃确认框保留 | [差异表 N8-11 W6](behaviour-changes.md) |
| U11 | 订阅 hosts 内容身份（HostDigest）改为原生编码 | 订阅 hosts 身份报告一次变化；不是设备文档的字节签名，不需要额外发布 | 否（随 U01 的全量推送一并消化） | [差异表 N8-9 subs typed hosts 元数据、合并与 D2](behaviour-changes.md) |
| U12 | 自动规则同步防抖键（A16 state.key）一次自动重建 | 先查询旧已接受任务，不提前接受新源；用户无感 | 否 | [差异表 A16、R13](behaviour-changes.md) |
| U13 | 上游 tag 等元数据含真实 U+2028/U+2029 的分流选择失配 | 自动编辑/重建此类分流时 400/409/warning，原设置保留 | **是**（仅极少数配置）：template 模式重新选择；native 完整配置按当前上游重新生成分流 | 附录 A12；[差异表 D2/C27](behaviour-changes.md) |
| U14 | 签名密钥：未设置正式 `JWT_SECRET_KEY` 时每次启动随机生成 | 每次重启使登录会话和待确认的固定 IP token 失效 | **是**：首次启动 Go 前设置随机、≥32 字节的正式密钥并长期保存；换密钥后重新登录一次 | 附录 A13；[差异表 O9-M3 与 R6](behaviour-changes.md) |
| U15 | Python 生成的旧安装链接失效 | 安全安装器生成器指纹变化，旧链接下载/校验失败 | **是**（要用旧链接时）：在 Go 面板重新生成，私下传递 | 附录 A15；[差异表 L1 D3、O9-H2 R4](behaviour-changes.md) |

## 2）行为变化

### 2.1 受影响部署需要处理

只有符合条件的部署需要操作；其余用户无感。

| 编号 | 事项 | 对用户的影响 | 需要操作 | 依据 / 详情 |
|---|---|---|---|---|
| P01 | 同一 DATA_DIR 只能运行一个服务进程，关机有排空时间 | 第二个进程拿不到实例锁，30 秒后退出 1；默认排空 8 秒，第二次信号立即退出 | **是**：切换时停掉 Python 及其调度器，等进行中的设备任务结束；停止服务用 SIGTERM 并留足 10 秒 | 附录 A02；[差异表 O3、R12、R17](behaviour-changes.md) |
| P02 | 开启登录后，新 Agent 首次注册需要授权 | 手拼无授权命令、旧二进制/旧镜像无法完成新首次注册；已有 Agent 用自身 token 不受影响 | **是**（新装/重装时）：从 Go 面板生成安装命令并尽快执行，使用本次配套 Agent 程序/镜像 | 附录 A14；[差异表 O9-H2 及复核补充](behaviour-changes.md) |
| P03 | 强制 DNS 遇到未登记归属的 `cf-dnsguard:hijack` NAT 规则 | 检查显示未知并阻断预览写入（Python 可能按注释误启停外部规则） | **是**（出现提示时）：人工核实外部规则，改用非保留注释后重新预览 | 附录 A16；[差异表 R09](behaviour-changes.md) |
| P04 | WebDAV 地址或备份路径含 `.`/`..` 路径段 | 保存、测试、手动备份 400，自动备份记录同一中文错误 | **是**（仅此类配置）：改成普通目录后保存并测试 | 附录 A17；[差异表 C35](behaviour-changes.md) |
| P05 | 依赖 Python 环境代理特殊规则（空小写变量屏蔽大写、`:port`、`.example.com` 同时匹配主域等） | 代理/直连选择可能改变或报代理配置错误 | **是**（仅此类部署）：按附录 A18 调整代理变量 | 附录 A18；[差异表 N2](behaviour-changes.md) |
| P06 | OxiDNS 旧动态设置中错误类型的 `custom_matches[].enabled`、`ros_route.args.*` | 兼容读取不改存储，但部分匹配/路由功能可能停用 | **是**（出现路径告警时）：改成正确类型后保存 | 附录 A19；[差异表 N8-9 DNS](behaviour-changes.md) |
| P07 | 损坏/手工改坏的存储记录不再被当作空值静默放行（fail closed） | 相应操作返回带中文说明的 400/409/500，不写入、不碰设备；其他 Agent/设备/日期照常 | **是**（只在出错时）：按提示用现有入口恢复——统计面板"重置统计"（B74）、`POST /api/agents/<id>/domain-discovery/clear`（B78）、流量历史清空（B78）、停用 HA（B73）、升级记录按 `doc/manual/agents.md`（B76）、错型 profile 绑定用编辑或带 profile 重注册修复（B71）、config_token 改回字符串（B81）、路由守卫配置修正后再操作（B69） | 见各 B 条；[差异表 N8-10、N8-11 W3](behaviour-changes.md) |
| P08 | HA 主备一台永久离线时 journal 卡在 unknown（继承自 Python，强制停用尚未实现） | 停用、推送、删除 Agent 被拒 | **是**（出现时）：按停用提示强制清除记录，再到设备上手动删除 keepalived 中的 ConfigFlow 配置 | M07；[A18 复核 O-1](review-a18.md)、[设计 a16-a18 "Clarified 2026-11-18"](../design/a16-a18-sync-follow-ha.md) |

### 2.2 一般行为变化

下列各表条目**均不需要用户统一操作**；普通面板操作不受影响，只有非法输入、损坏数据或异常设备回包会得到更明确的 400/409/415/416/502 或 fail closed。表中提到的"见 Mxx"指第 0 节。

#### HTTP、认证和安全出口

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| B01 | CORS 仅向请求自身 scheme/host/port 相同的 Origin 发许可头；无 Origin 不发 `*`，foreign/null/畸形 Origin 不发许可头。改变响应头，不拒绝请求、不改状态/正文，不信任 X-Forwarded-* 放宽；同源面板和 TLS 反代仍正常。跨源浏览器集成的影响见维护者确认 M02。 | [差异表 O9-H1](behaviour-changes.md)、[O9 修复](progress/o9-fixes-1.md) |
| B02 | 开启登录的受保护入口先认证再解析 profile：匿名统一 401 且不回显 `X-ConfigFlow-Profile`，不再先暴露非法/不存在 profile 的 400/404。已登录、公开入口、关闭登录仍按原 profile 合同。 | [差异表 O9-L6](behaviour-changes.md) |
| B03 | 非法 base64url 字符的 JWT 返回 401；Python 解码器可能忽略垃圾。合法 HS256、无 exp、分数日期等已有会话合同不变。 | [差异表 N8-7](behaviour-changes.md)、[N8-7 交回](progress/n8-7.md) |
| B04 | 锁定注册时，无有效首次注册证明的已有/未知身份统一 403 中文提示，不再对已存在 name/host 回 401 而泄露存在性；有证明的身份冲突仍 401、不消费凭据。旧 Agent 自身 token 授权与 A14 一致。 | [差异表 H2 匿名注册补充](behaviour-changes.md)、[O9 复核 R1](review-o9-fixes.md) |
| B05 | Edge 与 App 文本响应增加精确 `X-Content-Type-Options: nosniff`；安装命令短链接错误仍是带换行的原中文纯文本、no-store/nosniff，但移除错误响应的 attachment 头。 | [差异表 O9-L5、Step B/L1 D8](behaviour-changes.md) |
| B06 | `/api` 的原 HTML 错误改为同状态 `{success:false,message}` JSON，保留 Allow 等头，HEAD 无 body；不回显内部诊断。已明确的 shell 短链接纯文本错误保留，非 API HTML/重定向不在此类。 | [差异表 Step B #3](behaviour-changes.md) |
| B07 | 二进制下载无法满足的 Range 从被 catch-all 吞成 500 改为 416，带 `Content-Range: bytes */长度`，不带附件头、不记服务器 ERROR；正常 GET/HEAD、200/206/304、404 合同保留。其他历史 Range 差异见 M05。 | [差异表 L1 D7](behaviour-changes.md)、[L1 复核](review-l1-installer.md) |
| B08 | 后端端口与 Edge 统一请求体限制：通常 1 MiB，含 profile 的 OxiDNS settings/render 为 96 MiB；已知长度、chunked/stream 超限均 413，不能经后端直连绕过。 | [O9 修复 M1](progress/o9-fixes-1.md)、[差异表 O9-M1](behaviour-changes.md) |
| B09 | 规则下载/缓存刷新逐解压层及最终正文均限 64 MiB，不受压缩体积、Content-Length、streaming 绕过；更小的部署文件限制仍生效。超限拒绝且不缓存部分内容，保留上一份验证内容。订阅/Sub-Store 路径同样限 64 MiB（B63）。 | [差异表 batch-8 O9-M2](behaviour-changes.md) |
| B10 | 内部规则 URL 在原始分类、规范化及每次 redirect 都守住 origin/API 边界；歧义 dot/编码/反斜杠逃逸更早 400，IDNA/punycode/全角面板域名也按内部处理，禁止目标零请求。合法内部转向、外部来源、名称中的普通反斜杠保留；无 host 的 `http:ok` 按原生 URL 规则拒绝。 | [差异表 CFG-01](behaviour-changes.md)、[安全复核](review-cfg-c14-security.md) |

#### 请求类型、存储容错和诊断

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| B11 | 已迁移 JSON binder 拒绝 NaN/Infinity/-Infinity，包含未使用的成员；畸形/非对象等多条 Python 内部 500 改成明确 400，严格 MIME 错误改为 415。覆盖登录、间隔、C06 设置、C07、C29、C33、C34、discovery、A20 等已登记调用方。不是所有路由一律 400：C33 明确捕获解码异常的 cache/reorder/proxy-domains/preview-regex 等仍按原 malformed500；按路由保留 silent 业务文案。 | [差异表 N8-6 与各 C/N8 节](behaviour-changes.md)、[N8-6 交回](progress/n8-6-httpx.md) |
| B12 | nodes/subscriptions reorder、subscription fetch 先验证对象/排序字段，null/空数组/false/0/空字符串等不再作空对象或崩溃；400、无上游请求/写入，真正未知 ID 仍 404。server-domain/config-token 的孤立 UTF-16 转义 400 拒绝，不静默替换提交；合法代理对、显式 U+FFFD 不拒绝。 | [差异表 Step B #1/#2](behaviour-changes.md) |
| B13 | 五引擎 custom-config 拒绝非 null 的非对象根（Python 部分是无字段更新200/revision+1，部分500）。null/缺失 body 已撤销统一处理，仍逐引擎匹配 Python：Surge/Loon null200、Mihomo/MosDNS null500、四者缺失500；OxiDNS 两者400。`{}` 也继续各自原业务语义，不得当成安全空更新。 | [差异表 C34 对象契约](behaviour-changes.md)、[C34 复核](review-c34.md) |
| B14 | `base_url` 缺失/null 不覆盖，字符串（包括空串）照常，其他类型400且无 DB/文件/生成缓存提交；不再把 None/False/数字/容器拼成生成链接。涵盖预览/下载/artifact 的已有 C34 边界，不把该 schema 扩展到其他字段。 | [差异表 C34 base_url](behaviour-changes.md)、[artifact 交回](progress/artifacts.md) |
| B15 | cache `rule_ids` 必须数组；preview-regex 的 truthy subscriptions/aggregations 选择必须数组，避免 Python 逐字符/逐键误选。后者空值仍空选择，数组里不命中的非字符串继续忽略；cache 数组未知值仍忽略，完全未匹配404。聚合生成的 `subscriptions` 也是字符串数组，不再把 `"s"` 拆开选择。前端已传数组。 | [差异表 C31、C33 cache/preview](behaviour-changes.md) |
| B16 | MosDNS 数值/布尔写入更严格：缓存整数拒绝截断 float、bool、溢出和带空白/加号/下划线/小数的字符串；严格十进制整数字符串仍可用，bool 字段拒绝0和显式 null。非 JSON、畸形/数组、非 cache 的 null 等旧500变400；不存在 ruleset 引用500→409、同中文并加 usages。整个 cache-settings body 为 null 仍是原有效重置200。字符串 custom_matches 项仍可保存/显示、生成忽略。 | [差异表 MosDNS 请求边界](behaviour-changes.md)、[DNS 复核](review-n8-9-dns.md) |
| B17 | discovery apply/probe 非对象根500→中文400；11个写入口的非法/非有限 JSON 在写前400。旧 `auto_probe:1` 等错型 bool 按默认 false，下次保存写回；忽略域名列表过滤无效类型/主机名，未知设置/历史字段往返保留。 | [差异表 N8-9 discovery](behaviour-changes.md)、[PROGRESS-config C37](PROGRESS-config.md) |
| B18 | 共享 R5：typed 文本接受无损数值 token（`1e3`→`"1e3"`，不仿 Python `"1000.0"`），null 作缺失，bool 不作文本；model.Int 接受严格 ASCII 十进制整数字符串。R8 整数值浮点写法用于已声明允许的字段。未知字段/错型原始字节及未改成员保留，不因兼容读取自动回写；权限凭据仍须原始字符串。例外及影响不明项见 M03。 | [差异表 N8-8a 最终标量裁定、batch-6](behaviour-changes.md)、[model 进度](progress/n8-3-model.md) |
| B19 | 生成输出身份字段错型明确拒绝，避免空 provider/规则、策略丢失：Node.Type、order/strategy type、rule_type/value（MATCH 不查 value）、subscription/aggregation provider name、ruleset name/behavior、group type/follow_group、dialer_ref、definition/override/非文本 dialer-proxy 等。数字 node/profile name 则仍按共享 String 读取。诊断含资源/字段/原 JSON，不靠 R5 零值绕过拓扑。 | [差异表 N8-8a/8b 复核与 batch-6](behaviour-changes.md)、[provider 复核](progress/review-n8-8a-provider-fix-zh.md)、[8b 复核](review-n8-8b.md) |
| B20 | 其他 typed 消费使用 schema 缺省或明确错误，保留原始存储：错误形状的 interval/health_check/custom text/规则列表、EasyTier 根/生成节/version、discovery 身份、health/refresh 回执等不再进入 Python 动态 truthiness/容器遍历。类型错误的存储开关按 B85 的 JSON 真值统一规则（少数已批准字段例外）。损坏 rule library 不能证明 IP presence 时返回 unknown，避免借零值读取缓存。 | [差异表 N8-8a、8b、N8-9 health/rules](behaviour-changes.md) |
| B21 | 数据库打开只查结构，名称/revision/引用/Agent 绑定等业务错误推迟到写入；错型值可容错读并留原字节，修复前无关写入仍被校验拒绝。无效业务值不触发启动 token 维护写入，避免 Python 整体打不开或误改坏数据。 | [差异表 N8-5a](behaviour-changes.md)、[N8-5a 复核](review-n8-5a.md) |
| B22 | 三方合并按 JSON 类型区分 `1`、`1.0`、`true`、`0`/`false`；修订号接受整数值的浮点写法，仍拒绝负数/字符串/null/缺失，bool 仍沿原写入合同。路由连接保护也认类型变化，畸形发布记录仍阻止删除/改地址。名称错型用 JSON 表示；被引用资源名称坏类型时显示 ID。 | [差异表 N8-5a/b/c、C34 false/0](behaviour-changes.md)、[N8-5 交回](progress/n8-5.md) |
| B23 | 共享节、domain-discovery 设置/历史、stats_history 根等未知成员在更新中保留，修复 Python 丢字段。旧格式导入的部分错误 shape 从内部500变明确400或可正常转换。 | [差异表 N8-5b/c、Step B R058-B03、N8-7b](behaviour-changes.md)、[PROGRESS-config C37](PROGRESS-config.md) |
| B24 | 普通 JSON 响应的 NaN/±Infinity 输出 null，图表视为缺失。旧 metrics/traffic/task-status/notifications/stats 文件按值修复 nonfinite，保留有限兄弟值/数字原文，首次重写为合法 JSON；summary 跳过缺失/不可表示样本，全缺失为null，流量投影跳过坏计数、保留 raw。超大统计点不再因绘图 float 溢出500；原生数字范围可超过 Python 4300位及float64。文件/stream 透传例外不改变，回退风险见 A20。 | [差异表 N8-1、N8-7 两批](behaviour-changes.md)、[N8-7b 复核](review-n8-7b.md) |
| B25 | CPython-only 字符规则改用 Go Unicode/ASCII 语法：部分 C0 首尾字符不再 trim；任务计数拒绝非 ASCII 十进制数字，但合法 ASCII 计数中的符号/单个数字分隔下划线仍按合同读取；监听/sniffer 端口使用原生 ASCII 解析，`CONFIGFLOW_NODE_TEST_TIMEOUT_MS` 不接受下划线/Unicode数字。provider 改名中的非 ASCII `$n` 保留字面量，down 非 ASCII 数字不作带宽。日志保留 Unicode 新增可打印字符，仍过滤控制/换行/bidi。MosDNS 首尾 U+001F 可进入规则文本，手写此类规则应移除控制字符；普通空格/合法 Unicode 空白不变。 | [差异表 N8-7/8a/8b/8c、N8-9 health、MosDNS 空白](behaviour-changes.md)、[C07复核F1](review-c07.md)、[PROGRESS-config 原生规则](PROGRESS-config.md) |
| B26 | 正则直接用 RE2/regexp2：word boundary/`$`/空串 `\B` 依各引擎；部分 variable-width lookbehind、非起始 flags 等可用；超 int32 repeat 不支持，回溯预算1秒后不匹配。规则文件只按 LF/CRLF/CR 分行，VT/FF/NEL/U+2028/U+2029 等不是 Python 式分隔符。`\N{name}` 的残余行为与 Unicode/IDNA 少见输入见 M01。 | [PROGRESS-config 原生 rules 决定](PROGRESS-config.md)、[review-c19-c20](review-c19-c20.md) |
| B27 | 原生错误原因替代 Python exception/repr：保留已约定中文上下文与业务状态，YAML 增加可用的一基行列；多文档/无类型名标签有中文位置提示。错误原值以 `null/true/[...]` 代替 `None/True/repr`，设备多字段不一致按排序后的首字段提示。health worker 非法 UTF-8 固定中文，不回显 worker 内容；R13 意外读取失败用「读取失败：内部错误」。 | [差异表 N7-2/3d、N8、A23、R13](behaviour-changes.md)、[R04](review-r04.md)、[R08](review-r08.md)、[A11](review-a11.md) |

#### YAML、生成和缓存可靠性

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| B28 | yaml.v3 原生隐式类型：`yes/on/sexagesimal` 等通常是字符串，整数显式标签保任意精度十进制；timestamp/binary/nonfinite 到 JSON 边界保原文本，未知及 `!!python/*` tag 保节点/文本，不执行 Python 构造器。`0123` 在 OxiDNS 新共享读取中为八进制83。合法 JSON 优先按 JSON 解码，代理对 emoji 有效；非 JSON YAML flow 的孤立 surrogate 不修补。 | [差异表 N7-3/3c、N8-9 DNS](behaviour-changes.md)、[N7-3a](progress/n7-3a.md) |
| B29 | 重复 key、非法显式 tag/内容/merge、自引用或超预算 alias 拒绝；MosDNS/OxiDNS 和路由 desired 仅真正语法错误保原 fallback，policy/alias 错误不得静默用默认模板/Fake-IP。budget 内共享 alias 保留；普通深度/anchors/TAB/BOM/directive/escape 按原生 grammar，部分旧接受输入拒绝、部分旧拒绝输入接受。输出超预算或 `1e400` 这种无法按自身 float tag 读回的标量明确中文拒绝。 | [差异表 N7-3/3b/3c/3d、HR refusal、shared writer](behaviour-changes.md) |
| B30 | YAML writer 两空格、LF、不折行；必要引号/标签保住 `0o17`、`1e3` 字符串、大整数精度与 timestamp 类型。Compose 的 NEL/U+2028/U+2029 改为转义，避免 PyYAML 后续 splitlines 破坏值。JSON→Node 不再把合法 JSON emoji/数值样文本误作 YAML。一次配置字节变化合并在 A04。 | [差异表 N7-4、C31 JSON bridge](behaviour-changes.md) |
| B31 | 编辑先作安全 scalar/pair splice，其余需要时整体重排：保注释、规则次序和值，空白/引号/缩进可能在实际编辑时归一；无变化/只读不重写来源和缓存。CRLF 注释文档可安全编辑，folded 用 literal 保值，复杂行注释可移到 pair 前，含 TAB 字符串用双引号；移位/删除 anchor 后重新安置定义。 | [差异表 N7-2/3b、displaced anchor](behaviour-changes.md)、[N7-2 复核](review-n7-2.md) |
| B32 | 修复清空无缩进 payload 后的非法空列表、flow/alias 列表追加顺序、前导空行/注释/`---` 隐藏 flow payload 的格式判断；修复后 MosDNS/OxiDNS 也能正确取规则。物理跨行 quoted 规则按 YAML 折叠接受，折叠后含空格仍由域名规则拒绝。普通 Classical 文本不套 YAML mapping 校验。 | [差异表 N7-3b](behaviour-changes.md)、[N7-3b 复核](review-n7-3b.md) |
| B33 | structured-proxy 的复合键不能过 JSON 边界；标量键统一投影，冲突后值覆盖、保最初位置，可能不同于 Python 带类型键。共享 parser 保非字符串 Node 键，string schema 忽略无关键；完整集合/unsafe alias 处理依 B29，不沿用旧“都转 Sub-Store 重试”的历史规则。 | [差异表 N7-3b/c](behaviour-changes.md)、[PROGRESS-config C12 历史键差异](PROGRESS-config.md) |
| B34 | C33 DB/cache 发布失败时阻止旧稳定/legacy 内容继续交付；持久失效标记下从当前 DB/inline 重建或直交付，I/O 异常不作标记不存在。并发迟到 inline/暂存不能覆盖新声明；下次编辑或手动缓存修复标记，不额外启后台恢复。 | [差异表 C33 缓存协议及复核](behaviour-changes.md)、[C33 复核](review-c33.md) |
| B35 | 规则库批量连通性测试结束后重读事务，只关闭仍与测试快照相同的来源，复用 native DNS rebuild/parking；不覆盖测试期间用户编辑。 | [差异表 C33 可靠性](behaviour-changes.md) |
| B36 | 聚合预览取本次已校验不可变节点快照，不在发布后重读可能被替换的 provider；错误类型 nodes 缓存不再被线程吞错变成成功空 provider，拒绝且保留旧发布内容。 | [差异表 C31](behaviour-changes.md)、[C31 交接](C31-HANDOFF.md) |
| B37 | Surge WireGuard 数字 reserved 列表输出 `1/2/3`；TUIC 字符串 ALPN 保留/单字符串列表展开，多个或坏元素列表省略并警告；url/interval null 或 follow 继承缺失用默认测速 URL/300秒，避免 `None`。UDP/TLS 等仅 bool，字符串 `"false"` 不再为真，错误类型用默认并警告。 | [差异表 N8-8c](behaviour-changes.md)、[8c 交回](progress/n8-8c.md) |
| B38 | 引号节点 port 按 Int 生成整数（`"00443"`→443），原存储字节不动；超范围 region_probe_port 提前中文拒绝，不交给核心加载失败；错型引擎选项不写输出，错型 config_token 不进入 provider URL。数字密码在 Mihomo 开放 Params/proxy_string 保原值/tag；数值 GitHub 代理域名会按文本启用前缀。 | [差异表 batch-6、N8-8b 复核、通用标量](behaviour-changes.md) |
| B39 | gateway 源 provider 的 scoped IPv6 /128 去掉 zone，避免 Mihomo 无法解析；URI `udp://` 等 scheme 不再被误认成同名插件引用，真实 `$udp`/jump 引用仍阻止不安全编辑，恢复合法独立旁路设备策略编辑。 | [PROGRESS-agents A19](PROGRESS-agents.md)、[A19 复核](review-a19.md)、[差异表 C26](behaviour-changes.md) |

#### 发布、设备、安装和运行状态

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| B40 | HEAD 修复按具体路由：settings、stats/notifications、OxiDNS settings、rules/DNS、rule library/draft、proxy-groups/domains、profiles/custom-config、gateway forwarding/onboarding、router collection/forwarding 等进入只读读取分支，不再意外保存/增 revision/创删资源或写设备；stats/notifications HEAD 不采样/刷新/迁移。client-devices HEAD 与 GET 同一 Agent 绑定空间。不是所有 HEAD 都变200：A23 保404，C31 的原500/不修改合同按各入口保留。 | [差异表 HEAD 各节、R15/R17/A21](behaviour-changes.md)、[C31 交接](C31-HANDOFF.md) |
| B41 | 公开 Agent/部署、profile 绑定、gateway/向导/IPv6/客户端发现/路由器报告、push-diff/搜索和错误/任务/日志均补强凭据脱敏：跨设备及历史凭据、URL userinfo/编码引用、元数据键/path 也保护。先捕获完整凭据再投影；用户名、状态/ID/has_password 协议值仍可见，私有存储和可恢复完整管理员备份保全。用户配置 reveal 权限沿原合同。 | [差异表 A24/A13/A20/C34/C36/R15/R16/A23 及日志节](behaviour-changes.md)、[A12 复核](review-a12.md) |
| B42 | WebDAV 编辑地址移除 userinfo，诊断/通知脱敏；表单空密码与 `******` 都保留旧密码，不再空串清空，真实新密码仍替换，未新增清空入口。test/now 的空密码校验不随之改。 | [差异表 C35 凭据](behaviour-changes.md)、[C35 复核](review-c35.md) |
| B43 | 安装器覆盖前备份原程序/配置/token/服务状态，失败停止新服务再原子恢复；停不掉旧 Agent 时拒绝覆盖。全新失败清本次新建文件/目录，保原目录；外壳等 HUP/INT/TERM 补偿结束再输出结果，OpenRC 恢复进程脱离安装器进程组；发现旧 `.agent-install-backup.*` 提醒、保留、不自动套用。 | [差异表 L1 D3 及补记](behaviour-changes.md)、[L1 复核](review-l1-installer.md) |
| B44 | HA 新 Host 安装检查 Docker daemon 空间 UDP/TCP53，已证归属且运行的主容器续装跳过自占用探针；新装/迁移需 Supervisor OxiDNS RUNNING 才成功，坏状态/30次等待后非零退出。验证命令按 dig→nslookup→BusyBox 选工具，始终查指定 IPv4 VIP，无工具退出127并提示包名，查询失败不偷换默认 DNS。 | [差异表 L1 D5/D6](behaviour-changes.md) |
| B45 | compact Compose 与其他方式一样拒绝含冒号的数据目录，400且不签发 capability；Agent 名、Docker HA server_url、EasyTier URL 不再接受单个末尾换行。Docker HA 非法 IPv6 URL 用 net/url 原生原因；非法 UTF-8 query 原生替换，Shell 引用仍受校验。IPvFuture bracket host 不实现，合法 IPv6/zone 可用。 | [差异表 L1 D1、Step B #12](behaviour-changes.md)、[PROGRESS-agents 决定6](PROGRESS-agents.md)、[C05 复核](review-c05.md) |
| B46 | A14 同 deployment_id 的 final_config+冻结 Geo 重试按不含 resource 的内容身份比较，不再误拒重复内容、只上传一次；手动回滚若持久 rolling_back 后进程已无提交 owner，查询重发同一幂等受保护请求，不再永远等待。缺 profile_id 的回执匹配仓库实际默认空间。 | [差异表 A14、A11](behaviour-changes.md)、[A14 交接](A14-HANDOFF.md)、[A14 复核](review-a14.md) |
| B47 | A16 只把未 submitted 的准备异常记 failed，已提交交原轮询确认；HA enable 与设置提交竞争时事务回滚，OxiDNS 下载前重验绑定/设置，避免用旧来源。A17 同事务重验来源/目标/binding，空证明不授权写 upstream0；发布期间新草稿保留、成功可带 draft_synced=false。 | [差异表 A16/A17](behaviour-changes.md)、[A16](progress/A16-handoff-zh.md)、[A17 复核](review-a17.md) |
| B48 | HA keepalived 阶段锁忙保留 applying、下一轮重试，Python 原记整个 enable 失败（D-A18-1=A）。错误类型 handoff/released 证明拒绝继续核心写入，保 unknown/预留，不让 R5 零值绕过交接门禁。10分钟离线强停决定的落地待 M07。 | [A18 交回：设计决定](progress/A18-handoff-zh.md)、[总交接 O-1](../HANDOFF.md) |
| B49 | A20 坏 forward_filter（state/kind/source 错型）只让本条 gateway error、can_persist=false，不拖垮健康列表；managed 错型是不可读的已有记录，fail closed。坏服务草稿中文400而非空恢复快照；A22 setup 同类错误409而非500，私有会话2MiB上限且损坏拒绝。C36/A20 共享会话16MiB上限，损坏 ownership 不作“无记录”。 | [差异表 A20/A22](behaviour-changes.md)、[A22 复核](review-a22.md)、[C36 交回](progress/C36-handoff-zh.md) |
| B50 | A21 转换模板时备份原始 native 文档，恢复尝试的中间投影不再污染 backup。A22 installation capability 的 marker/完整身份在注册写事务内重验，避免并发 rebind 后复用旧证明。 | [差异表 A21/A22](behaviour-changes.md)、[A21 复核 F2](review-a21.md) |
| B51 | RouterOS surplus/duplicate/dynamic fresh IDs、缺失/错误证明、危险 key/超大成员等 fail closed；仅记录/回滚有证明的对象，32KiB限制也覆盖回滚重建；复合值走 REST，不作丢信息的摘要证明。R07/R08 迁移先整批验证，回滚失败停止后续删除并保依赖保护；ID移到别的列表不再报“已删除成功”。 | [PROGRESS-routers R04/R07/R08](PROGRESS-routers.md)、[R04](review-r04.md)、[R07](review-r07.md)、[R08](review-r08.md) |
| B52 | iKuai：丢失 route_comment 回复携带恢复 undo；route_delete 先算可用 undo 再删；原组消失后不把复用 gid 的外部组当自己的；缺 code 写回复记不确定并保补偿；restore 先核 tagname、防 ID 复用。畸形 verinfo/Location 变结构化502，失败 probe 保存、revision一次；UI netmask 后缀剥离后核验/补偿，不留新路由。 | [PROGRESS-routers R05](PROGRESS-routers.md)、[R05 复核](review-r05.md)、[差异表 R05 Step B/R15](behaviour-changes.md) |
| B53 | R03 iKuai 分组重排只排入尚未安置的新 CIDR，不重复计入已分配项；单项过长停止并报错，不无限循环；带尾换行的 cf 分组名不认领，未知 op ID 不触发 StopIteration。后几项的真实可达性见 M08。 | [PROGRESS-routers R03](PROGRESS-routers.md) |
| B54 | R09/R10/R11 使用完整稳定配置和表+ID 证明，预览后外改/保留名冲突/新同目标路由等写前拒绝，删除不吸收当前外改；部分恢复仍登记可信新 ID、保留原证明，不按相似配置猜归属。R10 借用路由仍须 active；补偿 DELETE 报错但 GET证实404按成功继续。R11 ID-only 旧记录需模板证明，用户 dhcp-client hook仍只认ID；ND坏状态不作可写，撤回后晚出现受管状态报不完整，不额外删现场。 | [差异表 R09/R10/R11](behaviour-changes.md)、[R10](review-r10.md)、[R11](review-r11.md) |
| B55 | R12 返程 apply/remove 前重证 Agent 持有的完整 spec，别的 router 改过则拒绝/回滚，不覆盖他人；不确定传输写保补偿，只以 GET 证明删除/完整 spec 为成功。cleanup 按显式(profile,router)排除，不漏同ID其他空间；R14 最终 verify 任意错误都补偿自己增删。 | [差异表 R12/R14](behaviour-changes.md)、[R12 复核](review-r12.md)、[R14 交回](progress/R14-handoff-zh.md) |
| B56 | R14 30秒下载总预算从连接前计，含重试/读体；接收前预算耗尽记 failed、按间隔重试，读体超时或>4MiB才暂停为超大小/时间，不生成空意图。R06 写锁等待≤10分钟、手动写有设备超时；R13 首调用者也最多等35秒、读预算25秒后取消，避免无界后台读取。 | [差异表 R14/R13](behaviour-changes.md)、[R06 复核](review-r06.md)、[R14 复核](review-r14.md) |
| B57 | R16/A23 混合坏行隔离、保健康连接/客户端，扫描不完整时未见旧设备保持 unknown；非数组整表仍失败。A23 不以不完整/>256行基线证明新连接，不把无效/溢出包计数作正数，空 server/坏 dialer-proxy 不占3个直连候选。R17 固定IP归属要求非空 server/interface/gateway和完整 expected，避免空scope误删外部对象。 | [差异表 R16/A23/R17](behaviour-changes.md)、[R16](review-r16.md)、[A23](review-a23.md) |
| B58 | R13 读取坏 router 单项按 R5 默认/跳过，不让一条坏记录拖垮全部提醒/其他发布；空 Agent ID 不关联空 gateway。C37 init targets 一次请求按首见顺序去重，每目标一份资源/缓存，不留孤立库项；跨请求仍新建及加名称后缀。discovery 混合目标结果用首见顺序而非 Python set随机顺序。 | [差异表 R13、CFG-06](behaviour-changes.md)、[PROGRESS-config C37](PROGRESS-config.md) |
| B59 | A12 内容比较按真实类型/完整值，不用显示截断身份；整数键→字符串键可报告 modified（Python `_json` 把二者混同）。规则摘要解析 JSON/quoted/flow payload，正确去注释/escape/BOM，无效结构回退size，避免错误规则计数。 | [A12 复核 F3](review-a12.md)、[PROGRESS-agents A12](PROGRESS-agents.md) |
| B60 | MCP 凭据必须原始 JSON 字符串；坏 config_token 不能当匿名空值，system_config坏容器连有效JWT也拒绝。arguments/params/字段用DTO、非有限JSON拒绝、ID拒分隔符/点目录、Oxi递归merge限128；非法method类型 -32600替代-32601，params=[]不当空对象。内部异常固定中文、失败日志脱敏。多步工具关机可已create但未reorder而返回-32603，需核对后再操作。 | [差异表 MCP](behaviour-changes.md)、[MCP 交回](progress/mcp.md)、[MCP 复核 L3–L6](review-mcp.md) |
| B61 | RouterOS共享HTTP的非Latin-1 Basic-auth凭据用UTF-8发送，Python requests原会UnicodeEncodeError；标准ASCII凭据不变。其他畸形token头/编码边界不概括为此支持，仍按M06确认。 | [PROGRESS-routers R04：Python bugs fixed](PROGRESS-routers.md) |

#### 2026-11-19 之后合并的变化（N8-9 subs/routers、N8-10、N8-11）

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| B63 | 订阅和 Sub-Store 的 HTTP 响应体及每一层 gzip/deflate 解压结果限 64 MiB，超限返回「订阅或 Sub-Store 响应内容超过 64 MiB 上限」，不返回部分成功；恰好 64 MiB 仍可用。Python 原无上限。与 B09 一起覆盖全部规则/订阅下载。 | [差异表 N8-9 subs/substore independent review](behaviour-changes.md)、[review-n8-9-subs](review-n8-9-subs.md) |
| B64 | 订阅解析：VMess `port`/`aid` 为 bool、小数、溢出或错误容器时拒绝该条 URI（不再当 0 成功）；URI 端口不接受下划线、Unicode 数字和超 int64 值；proxies 必须是对象数组，空串/空映射/null 元素明确中文拒绝（带原值），0 节点结果拒绝且不缓存；节点名 bool/容器按空文本。**有意保留**：订阅 Base64 宽松读取、机场组未知 type 回退 select。 | [差异表 N8-9 subs 原生客户端边界、typed 订阅解析、监听器筛选与 Base64、外部 dialer](behaviour-changes.md) |
| B65 | 订阅 hosts/DNS：VT/FF/U+001C–U+001E 不再被当成换行，含这些控制字符的 hosts 交给 YAML 报 parse_error；rule 模式 fake-ip-filter 不是列表时保留整个原配置并给 filter_not_list 提示（不再覆盖用户值）；DNS listener 端口和内核版本只认 ASCII 数字（如 `1.2.3.4:٥٣` 为 listen_unparsed）。 | [差异表 N8-9 subs native DNS 图、typed hosts](behaviour-changes.md) |
| B66 | 机场中转（dialer）：proxies/proxy-groups/组成员不是列表时返回带字段路径的中文错误，不回显整份订阅；重复子组不再指数展开（保留 50 targets/100 members 上限）；metadata 损坏、没有可确认成员时不再生成空中转组，保留该节点 missing 提示并暂不交付（修 Python 生成空组）。 | [差异表 N8-9 subs 外部 dialer、typed 中转组生成](behaviour-changes.md) |
| B67 | 内置 Sub-Store：非法版本文本（全角/阿拉伯数字、`2.1_0` 等）视为无有效版本；在线更新内部错误固定中文、不回显内容；关机打断更新时保留旧 VERSION 和完整备份；显式 TMPDIR/TEMP/TMP 不可用时报 RuntimeDirectoryError（含变量名、路径、中文原因），不再静默换目录启动第二份 runtime；损坏的 state 根在任何服务控制前拒绝。缓存清理把数字订阅 ID `5` 读作 `5.json`。 | [差异表 N8-9 subs/substore 缓存边界、私有 runtime 状态、在线更新、临时根目录、Agent 未知版本](behaviour-changes.md) |
| B68 | EasyTier：Agent 上报非空但无法识别的内核版本时拒绝生成，提示「Agent 上报的内核版本无法识别：<原值>，请升级或重启 Agent」，拒绝发生在修改文档前；空/未上报仍走原 unknown 路径（影响面见 M16）。Surge/Loon 数字 node type 明确报「节点 type 必须是字符串，当前值：5」（Python 为 AttributeError）。 | [差异表 N8-9 subs 跨包调用边界收口、Agent 未知版本生成边界](behaviour-changes.md) |
| B69 | 路由器：请求中的 `ip_rule_sets`/`ros_policy_rule_sets` 为 `false`/`0`/`""` 返回 400（Python 当空列表）；type/endpoint/username/password/Agent 引用必须是文本、verify_tls 必须是 bool、检查间隔必须是 JSON 整数；存储中坏选择列表在无关改名时 400 而不是写空；损坏的守卫配置若带启用开关或所有权记录，probe/routes/固定 IP/IPv6 设备操作先提示修复；routersplit 坏拓扑 409 `ROUTER_SPLIT_INVALID`；向导会话 `completed` 只认 `true`。 | [差异表 N8-9 routers / routersplit typed boundary](behaviour-changes.md) |
| B70 | Agent 回包：部署状态/成功回包不是对象、不是 JSON、含重复键、`success` 非 bool，以及 legacy push/restart 的坏回包，统一返回结构化 502 `INVALID_RESPONSE`（如「Agent 返回的发布状态格式不正确，请检查 Agent 版本和日志。」），不写库、不改 revision；不再静默当作 pending 或裸 500。部署查询/激活/回滚要求原始 journal 的 deployment/profile/service 身份完全匹配。 | [差异表 N8-10 core-2 malformed deployment status、direct Agent protocol、traffic machine domains](behaviour-changes.md) |
| B71 | Agent 注册/心跳：`profile_id` 只接受字符串/null/不传，其他类型 400「Invalid profile id」且不消费一次性凭据；版本非字符串、非白名单或超 64 字节 400 且凭据不消费，缺失/null 默认 1.0.0；心跳数值越界 400。存储中错型 `profile_id` 现在可以通过编辑或带 `profile_id` 的重注册直接修复（Python 报错）；traffic-report、client-notes、规则应用、device-policies 遇损坏绑定 400，不再落到默认空间。 | [差异表 N8-10 core-2 registration profile、explicit profile repairs、api/agents](behaviour-changes.md) |
| B72 | Agent 版本比较只认 ASCII 数字（人为 Unicode/超长分量比较方向可能变化）；流量统计域名按 Unicode 17 小写（只影响少数码点的统计键）；Agent 地址中的非 ASCII 主机名按 IDNA 校验，错误不回显主机名/凭据；Docker 地址识别不再接受带前导零的 IPv4 等 CPython 写法。 | [差异表 N8-10 core-2 machine version numbers、traffic machine domains、api/agents](behaviour-changes.md) |
| B73 | OxiDNS HA：存储的 `oxidns_ha_publication` 存在但不是对象时，规则更新拒绝并提示「OxiDNS HA 发布记录损坏，请先停用或修复主备配置。」（停用 HA 清除后恢复）；心跳上报新地址而 HA 字段损坏时不跟随新地址、每 Agent+字段 10 分钟一条脱敏告警，心跳仍 200。 | [差异表 N8-10 monitor malformed HA publication、upgrades HR 复核 L1](behaviour-changes.md) |
| B74 | OxiDNS 统计面板：pending checkpoint 或状态文件损坏、含重复键时返回 `DASHBOARD_STATE_ERROR`/500，不再静默丢弃检查点；用"重置统计"开始新周期，首次加载就失败时的管理员恢复 API 见 `doc/manual/oxidns-panel.md`。Mihomo release 版本非 ASCII 或超 64 字节返回 502，不覆盖版本缓存。 | [差异表 N8-10 monitor pending checkpoints、native machine release versions](behaviour-changes.md) |
| B75 | 安装器：危险目录比较前先 `path.Clean`，`//etc`、`/opt/../etc`、`//` 等别名不能绕过系统目录/根目录保护（400，既有中文提示）；`agent_ip` 为整数时 400；53 端口探测中含非 ASCII 空白且无法解析的行判 unknown，不再放行；安装会话 ID 非规范时 issue 409/enroll 401；会话 `expires=NaN` 失效；registry 含非对象成员时 500 且不写入。 | [差异表 N8-10 installer 各节](behaviour-changes.md) |
| B76 | Agent 升级：已存 `latest_upgrade`/`latest_deployment`/HA 守卫错型时返回 `CorruptStateError`/500，不自动清除（恢复步骤见 `doc/manual/agents.md`）；protocol-2 成功必须由同一个 worker 回包同时证明 update_id/agent_id/status（修 Python 误判成功）；upgrade-info 缺失/空 architecture 在预留前 409「更新文件未准备就绪」，不再遗留 queued；Agent `version` 为 null 仍可升级。 | [差异表 N8-10 upgrades](behaviour-changes.md) |
| B77 | 下载与查询：条件请求日期只认三种标准 HTTP 日期，无效日期忽略条件（通常 200）；cache flush 的 `tags` 含非字符串 400；查询整数（含 `GET /api/logs?lines=`、域名发现 `days`）只认 ASCII 十进制，非法值回落默认；Agent 二进制缺失仍 500，消息改为 Go 原生 OS 错误文本。 | [差异表 N8-10 api/agents、N8-11 W9](behaviour-changes.md) |
| B78 | 流量与指标：summary/stats/trend 跳过坏组件返回 200（Python 部分 500）；stats 的 `data_points` 只计可用样本；HTTP 中非整数浮点 ≥1e6 用指数形式（见 M19）；domain-discovery 流量文件损坏（含整份不可解析、`days` 错型）时该 Agent 当日上报 500 且不覆盖原文件，用 `POST /api/agents/<id>/domain-discovery/clear` 恢复（见 M18）；SQLite 计数器损坏 fail closed，用流量历史清空恢复。 | [差异表 N8-10 metrics / trafficstore、HR L2 协调裁定](behaviour-changes.md) |
| B79 | monitor：面板 `logs.after`、测速 `timeout` 的非 ASCII 数字 400（不发 Agent 请求）；Prometheus 指标中的非 ASCII 数值按未知（null）。 | [差异表 N8-11 W2](behaviour-changes.md) |
| B80 | 保存时 `_revision` 按存储字面量比较，手写 `3.0` 与存储 `3` 不等返回 409（前端只发整数）；订阅、聚合、节点各写入口的 JSON 任意位置出现 NaN/Infinity 返回 400，不写库/缓存/任务。 | [差异表 N8-11 W1 复核 L4、W3](behaviour-changes.md) |
| B81 | 存储的 `config_token` 只有 JSON 字符串算凭据，null/缺失为未配置；数字、布尔、数组、对象一律按损坏凭据 fail closed（即使关闭登录也返回 401，只放行内部调用和有效 JWT）。修复为字符串后恢复。 | [差异表 N8-11 W3 及 Opus 复核 L1 裁定](behaviour-changes.md) |
| B82 | 网站识别的 HTML 解析改用标准分词：控制码实体保留、无效数字实体变 U+FFFD、同一 meta 的重复 content 取首值、畸形 markup 按标准 HTML 解释（影响域名发现的网站标题/描述）。非字符串规则类型/DNS 选择的提示用 JSON 文本（`[null,"missing"]` 代替 `[None, 'missing']`）。 | [差异表 N8-11 W4](behaviour-changes.md) |
| B83 | 配置空间：新建/克隆/导入的 `description` 非字符串 400「配置名称和说明必须是字符串，名称不能为空」；create/PUT/clone/DELETE 的非法 JSON 或 NaN 返回 400 且不写/不删（DELETE 空请求体仍正常删除）；路由发布确认 NaN 400；settings 文本裁剪不再把 U+001C–U+001F 当空白；JWT 字符串数值声明只认 ASCII；`GET /api/backup/config` 保留存储中的原类型。全局 import/reset 的归属冲突 409 正文与 Python 相同。 | [差异表 N8-11 W5](behaviour-changes.md) |
| B84 | 规则 `policy` 显式为 null 或非字符串时不再套默认 PROXY，继续拒绝并在提示中以 JSON 文本呈现（`'null'`）；网关校验对 `nodes` 为对象/嵌套数组的错型存储放行（这些值本就不会进入生成输出）。 | [差异表 N8-11 W6](behaviour-changes.md) |
| B85 | 存储布尔开关（`enabled`、`library_enabled`、`log_enabled`、`cache_enabled`、`region_check_enabled` 等）统一按 JSON 真值：缺失用默认；`false`/`null`/数值零/`""`/`[]`/`{}` 为假，其余为真。这恢复了 Python 的读法，撤回此前 Go 部分包"非 bool 回落默认"或"只认字面 true"的偏差；少数已批准字段（Agent registry/HA、OxiDNS 存储投影、MosDNS 归档等）保持各自规则。IP 存在判断遇非字符串规则 ID/content 返回未知。 | [差异表 N8-11 W7](behaviour-changes.md) |

## 3）不再支持

| 编号 | 不再支持的内容 | 对用户的影响 | 需要操作 | 依据 |
|---|---|---|---|---|
| D01 | pre-SQLite 及旧拆分布局的数据目录（只含 `config.json` 等 legacy 文件） | Go 拒绝启动，提示先用最后一个 Python 版本升级 | **是**（仍在旧版的用户）：备份 DATA_DIR → 用最后一个 Python 版本启动一次完成迁移 → 再换 Go。不要只把旧 `config.json` 复制给 Go | 附录 A01；[总交接 Not ported](../HANDOFF.md)、[差异表 O12 retired startup migrations](behaviour-changes.md)、[O13 §4](o13-completeness.md) |
| D02 | 启动时的一次性迁移：损坏库重建、数据库旁变更的旧 JSON 导回、旧 metrics 目录迁移、managed/节点测速 DNS 一次性清理（#569） | 已有 SQLite 但尚未跑过这些迁移的数据，Go 不会补做 | **是**（同 D01）：切换前确认最后一个 Python 版本已成功启动过 | 同上；[O13 §2 S5](o13-completeness.md) |
| D03 | Python 运行调优环境变量 `CONFIGFLOW_HTTP_THREADS`、`CONFIGFLOW_MEMORY_TUNING`、`MALLOC_ARENA_MAX`、`MALLOC_TRIM_THRESHOLD_` | 设置了也不再生效 | 可选：删除这些变量；内存上限改用 `GOMEMLIMIT` 或 `CONFIGFLOW_MEMORY_LIMIT_RATIO` | [O13 §5](o13-completeness.md)、[packages.md Not ported](packages.md) |
| D04 | 容器内 Nginx 及其配置文件 | Go 直接服务前端和 API；改过容器内 `nginx.conf` 的定制不再生效 | **是**（仅做过定制的部署）：把定制移到容器外的反向代理 | 附录 C 第 1 项；[差异表 Web 监听](behaviour-changes.md)、[PROGRESS-config 决定 16](PROGRESS-config.md) |
| D05 | 退回 Python 后端 | 不提供回退路径和回退工具；Go 写过的文件（null 历史点等）Python 未必能读 | 否（停损按第 4.4 节）；附录 A20 仅作历史记录（M17） | 本次简报；[O14 补充](progress/o14-switch.md) |
| D06 | autoupdate 镜像 tag（容器启动时拉源码重建） | 待 M14 决定；建议下线 | 待定：若下线，使用 autoupdate tag 的用户改用普通版本 tag | [O13 需要维护者决定 1](o13-completeness.md) |
| D07 | requests/certifi 自带 CA 包和 `REQUESTS_CA_BUNDLE` | Go 使用系统 CA；只信任 certifi 而不在系统信任库里的私有/自签 CA 会校验失败 | **是**（使用私有 CA 时）：按 M04 的确认结果改用 `SSL_CERT_FILE`/`SSL_CERT_DIR` 或把 CA 装进镜像信任库 | M04；[R04 复核 TLS](review-r04.md) |
| D08 | CPython 特有的数字/空白写法：全角/阿拉伯数字、`1_000` 下划线、U+001C–U+001F 当空白、超过 Python 4300 位限制的特殊处理 | 机器数字、端口、版本、查询参数中这些写法按非法值处理（400、回默认或 unknown） | 否（前端和 Agent 只产生 ASCII） | B25、B72、B77、B79、B83；[n8-11-plan §3.3 裁定 2](../design/n8-11-plan.md) |

## 4）升级步骤与停损要点（O14 草案）

> 前提：#40 发布镜像完成（O13 唯一缺口），M04/M11/M16 已确认。维护者自己的生产切换先在 L6 演练一遍。

### 4.1 切换前

1. 确认当前运行的是最后一个 Python 版本，DATA_DIR 中已有可读取的 `configflow.sqlite3`（D01/D02）。
2. 停 Python 服务后做冷备份：复制整个 DATA_DIR（含 `configflow.sqlite3-wal`/`-shm`、`runtime.sqlite3`、traffic/metrics 目录、规则缓存目录）；另外保存一份 `/api/config/export` 导出和最近一次 WebDAV 备份。备份要能单独恢复到 Go，不是给 Python 回退用。
3. 在部署配置中写好随机、≥32 字节的 `JWT_SECRET_KEY`（U14）；如有私有 CA，按 D07 准备；删除 D03 中的旧调优变量。
4. 按 2.1 节检查本部署是否命中 P03–P06（强制 DNS 保留注释、WebDAV 点目录、环境代理特殊写法、OxiDNS 错型动态设置），能先改的先改。
5. 记录切换前基线：Agent 列表（名称、版本、部署状态、content sync）、各路由器自动同步状态、HA 主备状态、IPv6 验收结论。用于切换后逐项对照。
6. 等待正在进行的发布、回滚、固定 IP、HA、路由写入任务结束；记下仍未确认的固定 IP 预览、进行中的设置向导（U05、U10）。

### 4.2 切换与首次启动检查

1. 用 SIGTERM 停掉 Python 容器并等它退出；确认同一 DATA_DIR 不会再有第二个进程（P01）。
2. 用同一 DATA_DIR 启动 Go 镜像。日志检查：没有"legacy 数据目录"拒绝；启动横幅版本号正确（M11）；开启登录时没有"随机签名密钥"WARNING。
3. 访问检查：`/health`、登录、首页和 Agent 页加载；设置页各项与切换前一致；`/api/agents` 数量与基线一致。
4. 后台任务：任务状态页的定时刷新、规则自动同步、域名发现、统计历史等按原周期运行；Sub-Store 按需启动（首次订阅刷新时）。
5. 规则缓存：通过缓存刷新入口预热，确认所需远程规则均可用（U02）。

### 4.3 Agent 全量重推及确认

1. 逐台（HA 按现有主备发布顺序）执行一次完整配置发布（U01）。每台确认：发布状态成功；Agent 卡片显示核心已加载、版本正确；content sync 变为 `synced`。
2. OxiDNS：有 Python 时期 ready 未激活候选的，重新推送并校验后再激活（U03）。
3. 路由器：自动同步因基准暂停的，逐台"预览发布"一次（U07）；DNS 跟随重新选择绑定（U06）；使用 IPv6 的重新检查并验收（U04）。
4. 最终核对：所有 Agent `synced`，没有残留 `changed`；若某台持续 `changed`，查看该 Agent 的部署日志，不要用改指纹/清状态掩盖。重新出现的已忽略通知再确认一次（U08）。

### 4.4 出问题时如何停损（不回滚 Python）

- **网络不会因面板故障中断**：Agent 和路由器在 Go 面板停止时继续运行最后一次成功部署的配置。面板出问题时首先停止对设备的写入，而不是退回 Python。
- **停止写设备**：暂停受影响设备的自动规则同步，不再发起发布；若 Go 进程本身异常（反复写设备、启动失败），SIGTERM 停掉容器。
- **收集信息**：提高 `LOG_LEVEL` 重现，保存日志（Go 日志已对密码/token/订阅链接脱敏，外发前仍需复查），记录出错的 Agent/设备和操作。
- **数据问题**：停 Go → 用 4.1 第 2 步的冷备份整体恢复 DATA_DIR → 再启动 Go（同一版本或修复版）。恢复后切换后已经发布过的 Agent 会再显示一次 `changed`，按 4.3 重推。不要把旧备份交给 Python 运行。
- **设备写到一半**：路由器写入有补偿和归属证明，中断后用"预览"查看现场差异再决定；HA journal 卡在 unknown 时按 P08 处理。不能假定所有中断操作都已自动回滚。
- **修复前进**：问题定位后从 `go` 分支出修复版本，按正常门禁发布；必要时回退到上一个 Go 版本（前提是两个版本之间没有存储格式变更，发布说明需写明）。

### 4.5 公开发布额外事项

- 镜像：确认镜像架构不少于当前发布的镜像、`tzdata`、`ca-certificates`、Sub-Store 仍由 supervisord 按需启停、`DATA_DIR=/data`、SIGTERM 关停（O13 #40）。
- 版本：Go 服务、Agent 二进制、node-probe 同一提交构建并注入同一版本号（M11），否则 U03/P02 的证明和首次注册无部署保证。
- 说明：发布页附第 5 节的公开升级说明；D01 的拒绝提示要能让旧版本用户找到"先升级到最后一个 Python 版本"的具体版本号。

## 5）面向公开用户的升级说明草稿（中文）

> 发布前由维护者补入版本号、最后一个 Python 版本号、升级窗口，并确认第 0 节；以下不表示现在已经发布。

本次升级把 ConfigFlow 后端从 Python 换成 Go。已有 SQLite 数据继续使用，前端功能、Agent 日常功能和全部 API 保留。配置文件排版和部分输入校验会变化，请按以下步骤完成首次切换。

1. **旧版本先升级到最后一个 Python 版本。** 如果你的数据目录还没有 `configflow.sqlite3`，或很久没有升级，请先备份 DATA_DIR，用最后一个 Python 版本启动一次完成迁移，再换新版本。新版本不再支持旧数据目录，也不提供退回 Python 的路径。
2. **备份并停机切换。** 停掉旧服务后整体备份 DATA_DIR。同一数据目录只运行一个新服务；等正在进行的设备操作结束再切换。
3. **设置正式签名密钥。** 在部署配置里保存随机、至少 32 字节的 `JWT_SECRET_KEY`。不设置时每次重启都会让登录和未确认的固定 IP 预览失效（已经生效的固定 IP 不受影响）。
4. **预热规则缓存，然后逐台全量推送一次。** 新版本的配置排版和缓存标识会让每个 Agent 显示一次 `changed`。逐台推送，确认核心加载成功、最后显示 `synced`。旧的 OxiDNS ready 候选需重新推送校验。
5. **按需重做一次的事项：** 使用 IPv6 的重新检查并验收；DNS 跟随重新选择绑定；路由器自动规则同步若暂停，手动预览发布一次；进行中的设置向导放弃后重新开始；切换前打开的编辑页刷新后再保存。重新出现的已忽略通知看一眼再忽略即可，不需要为此重新发布。
6. **新装 Agent。** 使用新面板生成的安装命令和本次配套的 Agent 程序/镜像。开启登录后，没有注册授权的手拼命令不再可用；一次性命令 24 小时内有效，生成过多也会让较早的命令提前失效，请私下传递并尽快执行。已有 Agent 用自身 token，无须为此升级。旧安装链接需重新生成。

**部分部署需要额外处理：**
- 强制 DNS 检查提示未登记的 `cf-dnsguard:hijack` 规则时，核实这条外部规则并改用非保留注释，不要按注释批量删除或接管。
- WebDAV 地址/路径含 `.` 或 `..` 目录段的，改成普通目录。
- 依赖环境代理特殊写法（空小写变量屏蔽大写变量、单独 `:port`、`.example.com` 同时匹配主域）的，按升级台账调整。
- 使用私有/自签 CA 的，改为系统信任库或 `SSL_CERT_FILE`/`SSL_CERT_DIR`；`REQUESTS_CA_BUNDLE` 不再生效（待 M04 确认后定稿）。
- 改过容器内 Nginx 配置的，把定制移到外部反向代理；旧的 Python 内存调优变量不再生效，可改用 `GOMEMLIMIT`。
- 跨域网页调用 API 不再获得任意来源许可，请使用同源面板或同源反代路径。

**已知限制：** HA 主备中一台永久离线时，主备记录可能停在"未知"，停用和推送被拒。请按停用提示强制清除记录，再到设备上删除 keepalived 中的 ConfigFlow 配置（自动的"离线 10 分钟后强制停用"将在后续版本提供）。MCP 工具报 -32603 时，先查询当前状态再决定是否重试。

**新版本还修复了：** HEAD 请求误写数据、缓存并发覆盖、安装失败不能恢复原 Agent、设备归属核验不严、凭据在日志和公开接口中泄露、损坏数据被当作空值静默放行等问题。非法输入通常得到更明确的 400/409/415/502 和中文说明；损坏记录不再被静默覆盖，会提示用现有入口（如"重置统计"、域名发现清空、停用 HA）恢复。订阅、Sub-Store 和规则下载的内容（含解压后）上限为 64 MiB，后端请求体也有统一上限。

## 附录 A）一次性影响与需处理事项的详细做法

第 1 节 U 条、2.1 节 P 条引用的编号对应如下；"什么时候"指不做的后果出现的时机。

### A01　旧版本必须先经过最后一个 Python 版本

**变化：** Go 不做 pre-SQLite、旧拆分布局、旧原始缓存、损坏库重建、一次性 managed/node-test DNS 清理，以及旧 metrics 目录迁移；只含 legacy 文件的数据目录会拒绝启动，数据库旁变更的旧 JSON 也不再导回。

**怎么做：** 从旧版升级时，先备份整个 DATA_DIR，用最后一个 Python 版本启动一次，确认迁移成功并形成可读取的 `configflow.sqlite3`，再停 Python、换 Go。不要仅复制旧 `config.json` 等文件给 Go。

**什么时候：** 首次启动 Go 前。不做会拒绝 legacy-only 目录；已有 SQLite 但未完成的一次性清理不会由 Go 补做。

来源：[总交接：Not ported](../HANDOFF.md)、[差异表：retired startup migrations](behaviour-changes.md)、[PROGRESS-config：C03 与 Known accepted differences](PROGRESS-config.md)。

### A02　调整为每个 DATA_DIR 一个进程，并留出停机排空时间

**怎么做：** 停止旧 Python 服务及其调度器，确认同一 DATA_DIR 只启动一个 Go 服务进程；切换前等待正在发布、回滚、固定 IP、HA/路由写入的任务结束。正常停止发送 SIGTERM/SIGINT，留足既有 10 秒容器停止预算；不要以第二次信号替代等待。

**什么时候：** 切换及以后每次重启。Go 同时持有实例锁与 Python 四把调度器生命周期锁，忙时最多等 30 秒后退出 1；默认排空 8 秒、截止前 2 秒取消剩余工作，第二次信号立即退出。不排空可能留下待核对的设备写入或中断任务，需重新检查现场，不能假定所有操作都已回滚。R12 有已开始设备尝试后的本地失败记录特例，其他 owner 不因此获得继续写设备的许可。

来源：[差异表：O3、R12、R17](behaviour-changes.md)、[O3-6b 交回](progress/o3-6b-routers.md)、[R10 复核：F4](review-r10.md)、[R11 复核：F6](review-r11.md)。

### A03　保留、核验并预热规则缓存，不手工批量改摘要文件名

**变化：** D2 改用 Go 原生 JSON 摘要，影响远端规则缓存、过滤后的私有 provider 名称和路径。旧缓存仍是离线兜底，不能直接删掉。

**怎么做：** 保留旧缓存；在 Agent 首次拉取/推送前，通过正常缓存读取、缓存刷新入口预热并核验所需规则。新旧摘要由程序处理，不按文件名猜测后手工搬移。只有当前数据库里完全匹配的共享规则库或 profile-local URL 声明可以认领旧文件；拟议编辑、已删除来源、captured 旧参数、无效 receipt 均不能认领。

**什么时候：** 首次 Go 生成/全量推送前。正常可写 reader 校验后安装缺失的新文件，保留旧文件和旧 mtime，不改 DB/revision，不覆盖已有新文件、不延长 freshness；纯生成用 Peek，只读、不迁移。只读快照/迁移写失败仍可使用合格旧内容，留待下次可写读取迁移。不预热且没有可认领的有效缓存时，离线生成可能阻断或缺少规则来源；只换文件名不能证明内容可用。

来源：[差异表：N8-9 rules/D2、batch-8 profile-local 缓存](behaviour-changes.md)、[rules 交回](progress/n8-9-rules.md)、[rules 复核](review-n8-9-rules.md)、[O14 补充](progress/o14-switch.md)。

### A04　逐台全量推送一次，确认核心真正加载成功

**怎么做：** 完成 A03 后，对每个 Agent 做一次完整配置发布；逐台确认整包接收、核心加载、部署状态成功，最后确认 content sync 为 `synced`。

**什么时候：** 切换后第一次发布窗口。Go 原生 YAML/tar/gzip 与 D2 私有 provider 路径会改变字节和包摘要，每个 Agent 会一次显示 `changed`。不推送会继续运行原已部署配置，面板保持待同步；不要用改指纹或清状态消掉这次 changed，也不要把测试副本格式迁移当生产迁移。

来源：[O14-CHECKLIST：A14/§7.1](O14-CHECKLIST.md)、[差异表：N7-4、A11、D2 provider 路径](behaviour-changes.md)、[A14 交接](A14-HANDOFF.md)。

### A05　OxiDNS 的旧 ready 候选需重新推送、校验

**怎么做：** 对 Python 时期已 ready、尚未激活的 OxiDNS 候选重新推送并完成校验，再激活。

**什么时候：** 首次在 Go 激活旧候选前。RulesFingerprint 绑定 Go schema/构建版本；旧证明不再有效，激活会提示重新推送并停止，不发送激活请求。内容 Fingerprint 与校验证明是两回事，不能把原配置字节未变当作证明仍有效。

来源：[差异表：A11](behaviour-changes.md)、[A11 交接](A11-HANDOFF.md)、[PROGRESS-agents：A11](PROGRESS-agents.md)。

### A06　IPv6 重新检查、prepare，并重做一次验收

**怎么做：** 在 Go 重新检查 IPv6 readiness；使用新 check_token 执行需要的 prepare，然后重新做 IPv6 验收。查看旧报告作参考，不用旧摘要授权当前配置。

**什么时候：** 切换后、依赖 IPv6 就绪/验收结论继续操作前。R11 readiness 和 A23 target/settings/client/applied 摘要均改变；旧缓存/历史报告保留、标为陈旧。不重做则旧报告不能证明当前配置，新动作会要求重新检查；Go 新报告在输入未变时保持有效。

来源：[差异表：R11、A23、batch-7 D2](behaviour-changes.md)、[A23 交回](progress/A23-handoff-zh.md)、[R11 复核：F2](review-r11.md)、[总交接：batch-7 决定](../HANDOFF.md)。

### A07　重新载入固定 IP 来源设备并生成预览

**怎么做：** 重新发现/载入来源设备，重新预览固定或取消固定 IP，再确认；首次更换签名密钥时也执行此步骤。

**什么时候：** 切换后提交此前尚未确认的操作前。Python 的 600 秒预览 token 包含 before-image，Go 改为服务端重读并比对原生摘要，连接指纹也改变。旧 token 或密钥失效时返回 409「固定 IP 预览无效或已过期，请重新预览确认。」，发生在设备写入前；已生效 DHCP 绑定、设备记录和联网不受影响。不重新预览则无法提交待确认操作。

来源：[差异表：R17、O9-M3 R6](behaviour-changes.md)、[R17 交回](progress/R17-handoff-zh.md)、[O9 R5/R6 追加](progress/o9-fixes-1.md)。

### A08　DNS-follow 的旧绑定重新选择一次

**怎么做：** 在 DNS 跟随设置里重新选择绑定，核对来源/目标后保存，再继续跟随操作。

**什么时候：** 切换后首次使用旧 binding 前。A17 采用 A14 原生 CanonicalDigest/SettingsDigest，不再复用旧 Python 签名；不重选会因旧绑定证明不匹配而阻断跟随写入。

来源：[差异表：A17 / D-A17-1 → B](behaviour-changes.md)、[A17 交回](progress/A17-handoff-zh.md)。

### A09　路由器自动规则同步重新建立发布基准

**怎么做：** 对因旧 controls 不匹配而暂停的设备，手动执行一次「预览发布」，核对并完成发布，建立新基准。

**什么时候：** 切换后恢复对应设备自动规则同步前。R14 controls 改用原生摘要；旧回执可读但不匹配时保持 paused。不操作则该设备自动同步继续暂停。不要把此项与 A16 防抖键重建混为一谈，后者自动重新检查。

来源：[差异表：R14](behaviour-changes.md)、[R14 交回](progress/R14-handoff-zh.md)、[R14 复核：119 行附近 D2 决定](review-r14.md)。

### A10　重新确认一次出现的已忽略通知

**怎么做：** 查看重新出现的问题，仍需处理则处理；已知可忽略的在 Go 再确认/忽略一次。旧 router-check 标陈旧时重新检查/预览。

**什么时候：** 切换后通知首次出现时。notification fingerprint 与 RouterSignature 改变，仍存在的问题可重现一次；不确认会继续显示。Go 再次忽略后，同一 incident 不会反复重放。单纯通知签名变化不触发同步/发布、不新增 pending/waiting，也无需为此重新推送。

来源：[差异表：N8-7 second batch、batch-7 D2](behaviour-changes.md)、[O14 补充](progress/o14-switch.md)、[N8-7b 复核](review-n8-7b.md)。

### A11　重读旧编辑页与临时预览

**怎么做：** 保留未保存编辑内容后刷新/重新读取规则库草稿、路由策略工作区、手工路由和网关草稿预览，再生成新预览并保存；发生 409 时不要绕过 revision。

**什么时候：** 切换后首次保存之前打开的页面。C33 draft revision、R06 手工路由 revision/fingerprint、R15 工作区 revision、A20 preview token 均用原生 JSON/摘要；不重读可能首次保存 409 或预览过期。C33 token 不持久化，R15 `last_probe` 不再参与 draft revision；实际配置和旧恢复文档仍保留。

来源：[差异表：D2/C33、A20、R15](behaviour-changes.md)、[C33 交接](C33-HANDOFF.md)、[R06 复核：Accepted differences](review-r06.md)。

### A12　含真实 U+2028/U+2029 的上游标识重新选择或生成分流

**怎么做：** 仅对上游 tag 等元数据含这两个真实 Unicode 字符、旧 choice/source hash 失配的配置：template 模式刷新列表并重新选择；native 完整配置在编辑器按当前上游重新生成分流，不能在锁定旧图上只点一次重选。

**什么时候：** 切换后首次自动编辑/重建此类分流前。原生 JSON 转义改变 EntryKey；不处理会出现过期选择 400、严格保存 409 或宽松 warning，保留原设置/source/YAML，不再按形状把 fallback 猜成 remote。字面文本 `\u2028`/`\u2029` 不受此条影响，已有交付结构不自动改源。

来源：[差异表：D2/C27](behaviour-changes.md)、[C27 复核：D2 recovery](review-c27.md)。

### A13　设置并长期保存正式 JWT 签名密钥

**怎么做：** 生成随机、至少 32 字节的正式 `JWT_SECRET_KEY`，写入持久部署配置并保管；首次换密钥后按需重新登录，并按 A07 重做待确认的固定 IP 预览。即使未开启登录也应设置，因为固定 IP token 共用此密钥。

**什么时候：** 首次启动 Go 前；后续重启沿用。未设置、示例占位值或短于 32 字节会被启动随机密钥替代，服务仍启动；开启登录时记录无密钥内容的 WARNING。不设置正式值会每次重启使登录会话和待确认的固定 IP token 失效。正式密钥 ≥32 字节且未更换时行为不变。

来源：[差异表：O9-M3 与 R6](behaviour-changes.md)、[O9 修复及 R5/R6](progress/o9-fixes-1.md)、[安装说明](../../doc/guide/install.md)。

### A14　新装 Agent 使用新面板生成的命令及配套新版程序

**怎么做：** 开启登录后，从 Go 面板生成 Shell/AIO/Docker 安装命令并尽快执行；自动部署也可显式配置注册密钥。发布方必须同时提供由本次源码重建的 Agent 二进制和 Docker 镜像；用户新装时使用这些配套版本。失效、重放或身份不匹配时重新生成命令，不反复执行旧命令。

**什么时候：** 切换后每次首次注册/重新安装前。首次注册须有注册密钥、面板 JWT 或面板签发的一次性 24h 凭据；后者绑定名称、服务、端口、部署方式、profile，指定 IP 时还绑定 host，成功仅可消费一次，重启、删 Agent、备份恢复均不恢复使用权。最多保留 100 个未消费身份，满后淘汰最早未用项，较旧命令可能提前失效；Docker compose/run 共享同一组凭据，AIO 占两个名额。

**不做会怎样：** 手拼无授权命令、旧二进制/旧镜像无法完成登录开启后的新首次注册，返回重新生成/设置密钥提示。已有 Agent 仍用自身 token，不需仅为此强制升级；JWT/注册密钥不能接管已有身份。未开启登录且未设注册密钥仍允许匿名首次注册；未开启登录但设密钥时，新 Agent 可用 JSON `registration_key`，已有 Agent 用自身 token 即可重注册。

来源：[差异表：O9-H2 及复核补充](behaviour-changes.md)、[H2 交回及 R2–R4 收尾](progress/o9-h2.md)、[O9 复核](review-o9-fixes.md)、[Agent 安装指南](../../doc/guide/agent-install.md)。

### A15　重新生成旧安装链接，并把新链接作为临时密码保管

**怎么做：** 不继续复用 Python 生成的旧安装链接，改由 Go 面板重新生成。私下传递新安装命令/链接，不把含凭据脚本、Docker run 参数或 inspect 环境公开；确认目标身份后尽快安装。

**什么时候：** 切换后使用旧链接前、每次签发后。安全安装器的生成器指纹使旧不安全链接失效。新脚本快照和链接可持有明文一次性注册凭据（“仅存 hash”只指主库会话），Docker 参数/环境也可见；未指定 IP 时，首次使用前仍属于 bearer capability。不重新生成会下载/校验失败；泄露可能让他人抢先完成该身份的首次注册，首次成功后凭据即失效。

来源：[差异表：L1 D3、O9-H2 R4](behaviour-changes.md)、[L1 复核](review-l1-installer.md)、[H2 交回](progress/o9-h2.md)。

### A16　清理强制 DNS 保留注释冲突

**怎么做：** 若检查显示未知并指出未登记归属的 `cf-dnsguard:hijack` NAT，人工核实该外部规则，将它的注释改为非保留注释，再重新预览。不要仅按相同注释认领或删除规则；手动重建/WinBox undo 形成的新 ID 也须核对。

**什么时候：** 切换后对应设备第一次检查，或下一次启用/更新强制 DNS 前。不处理会阻断预览写入，即使该 NAT disabled、端口/chain 不同或未开自动暂停也一样；撤销不受此冲突阻断。Python 原本可能显示一致并按注释误启停它。

来源：[差异表：R09](behaviour-changes.md)、[R09 复核：Netwatch 归属裁定](review-r09.md)。

### A17　修正 WebDAV 地址或备份路径中的点目录

**怎么做：** 仅对含 `.`/`..` 路径段（含多层编码/编码分隔符）的旧配置，改成 `/backups/configflow/` 这类普通目录并保存、测试连接。

**什么时候：** 切换前或下一次备份前。不改则保存、测试、手动备份均 400 且无网络请求；自动备份记录同一中文错误，旧设置保留，不会自动改写成别的目录。普通重复斜杠仍按原规则归一。

来源：[差异表：C35 点路径](behaviour-changes.md)、[C35 复核](review-c35.md)。

### A18　核对依赖环境代理特殊规则的部署

**怎么做：** 依赖空小写变量屏蔽大写变量的，移除大写代理变量或用合适 NO_PROXY；把单独 `:port` 改成 `host:port`，同时排除主域和子域时用 `example.com`。必须经代理访问的 Agent 改用可路由地址；proxy URL userinfo 中的保留字符做百分号编码。不要在 CGI 的 REQUEST_METHOD 环境下依赖 HTTP 环境代理。

**什么时候：** 切换前检查部署环境，切换后首次连接失败时复核。不调整可能改变代理/直连选择或直接返回代理配置错误：localhost/loopback 始终直连；空小写值回退大写；`.example.com` 只匹配子域；CGI HTTP 代理拒绝并零请求。scheme 代理优先、ALL_PROXY 回退及显式下载代理仍可用；内置 Sub-Store 直连。

来源：[差异表：N2](behaviour-changes.md)、[N8-2 复核：proxy](review-n8-2.md)。

### A19　修复 OxiDNS 旧动态设置中的错误类型

**怎么做：** 受路径告警影响时，把 `custom_matches[].enabled` 改为 bool，把 `ros_route.args.routing_table/gateway4/gateway6` 改为文本后保存。

**什么时候：** 首次核对 Go 生成结果时，或下次编辑此类旧设置前。不修复时兼容读取不会改原存储：enabled 的数字 0/1 作 false/true，数字路由表/网关作原 token 文本；其他非布尔 enabled 停用该匹配，无效容器停用该 RouterOS 路由功能，可能使原功能不再交付。新 API 提交错型值返回 400、无 DB/文件写入。

来源：[差异表：N8-9 DNS](behaviour-changes.md)、[DNS 交回](progress/n8-9-dns.md)、[DNS 复核](review-n8-9-dns.md)。

### A20　仅在紧急退回 Python 时清理不兼容的 null 历史点（历史记录，已不适用）

> Python 已退休，不提供回退路径（D05、M17）。本条只保留供维护者例外决定时参考，不进入升级步骤。

**怎么做：** 常规升级保留历史，不做清理。若维护者决定紧急退回 Python，先备份，再从受影响 Agent metrics JSON/JSONL 去掉 nonfinite 修复形成的 null 点，或等待 24 小时保留窗过去；若损坏的通知 occurrences 从 nonfinite 变成 null，删除对应坏通知项，再由真实来源重新生成，不删除配置或任务文件。

**什么时候：** 仅紧急回退前，属于回退预案，不是常规升级步骤。不处理时 Python 可加载/保存文件，但 summary/traffic 算术或通知递增可 TypeError/500；通知无统一自动过期时间，不能靠等 24 小时解决。task-status/stats 的 null 计数可由 Python 当零使用；Go 可直接处理缺失值。新极大整数 JSONL 点还可能被 Python 的 4300 位限制跳过。

来源：[差异表：N8-7 两批回退注意](behaviour-changes.md)、[O14 补充](progress/o14-switch.md)、[N8-7 文件交回](progress/n8-7.md)、[N8-7b 复核](review-n8-7b.md)。

## 附录 C）纯内部差异（不列入发布说明，只计数）

用户无须处理，也不会在界面上看到。共 **26 类**：

- 2026-11-19 草稿已有 14 类：Python/Werkzeug/Nginx 运行与调优改为 Go 原生并嵌入资源；进程内锁取代各类 `.lock` 文件（旧文件保留不删）；Runtime 统一跟踪后台工作、断连不取消已受理批次；D2 内部发布/归属/缓存摘要原生编码；tar/gzip/ZIP/JSON 空白与键顺序；typed DTO 不改调用方副本；缓存准入预算按 Go 内存估计；SQLite `-wal`/`-shm` 存在时间与 WebDAV 内存编码；原生日志/OS/SQLite 错误文本；常量时间凭据比较与紧凑请求 JSON；R11/R12/R14 内部快照格式；H2 注册会话字段；无调用者代码不移植；artifact 一次捕获配置与 revision。
- 之后新增 12 类：Sub-Store 缓存清理日志后缀与 runtime `state.json` 原生 JSON；订阅临时对象 POST 的紧凑 JSON；Sub-Store 关机取消不形成上游 cooldown；监听器/筛选/中转组/链生成迁到唯一 typed owner（生成字节不变）；`mihomoenv` 生产接线去掉 JSON 往返；旧心跳库读取与坏 UTF-8 诊断原生化；原生 MIME/IANA charset codec；日志设置等写操作脱离浏览器断连；routersplit 不再仿造 Python 异常；metrics/traffic 存储 JSON 结构比较、SQLite schema 不变；repository 新写 JSON 对象的成员顺序/空白（字段 position 保持）；W9 删除 jsonx/pystr 与全部 legacy 门面。
- 另有早期接受、M06 关闭的 HTTP 细节（不读 `.netrc`、无 BOM 的 UTF-16/32 不猜测编码、redirect 次数按 Go 默认）。

来源：原草稿 C01–C14 各自出处（[差异表](behaviour-changes.md)、[O3-6](progress/o3-6-locks.md)、[O3-6c](progress/o3-6c-shutdown.md)、[artifact 复核](review-artifacts.md) 等）；新增项见差异表对应 N8-9/N8-10/N8-11 小节。

## 来源冲突与已撤销记录的处理

- A08/A09旧surrogate、异常Host500、非字符串session token等历史接受项不直接宣称仍是正式合同，随最终原生边界归M06/M10；server-domain/config-token孤立surrogate明确拒绝则保留B12。
- C34曾考虑统一null/缺失为无更新，独立复核已撤销，采用B13逐引擎真实合同；`config:false`→`config:0`仍是N8-5类型合并差异，不恢复Python相等规则。
- O9复核R2最初描述凭据满额503，后来已以format=both共享凭据并淘汰最早未用项修复；采用A14，不再提示“等待24小时才能生成”。R3–R6的文档/警告/固定IP补充也以最终追加为准。
- R04早期“ApplyMany批次间响应浏览器取消”由O3-6b最终Detach决定覆盖；真实Runtime关闭仍取消，不扩大R12的本地记录特例。
- A21缺C36 owner503、artifact deferred、原N8-9 rules“实施中”、A14共享commit窗口、CFG-08依赖未接线等历史进度不作为永久差异；合并树确认归M09。
- 测试副本格式转换、随机ID/时间与端口逐端验证、生产比较器中的精确baseline映射只是验证办法。不得在生产改Fingerprint、清changed或丢弃完整字段来模拟“无差异”。

## 维护者裁定（2026-11-22）

- M01 接受：面板内部的正则/IDNA 生僻写法按 Go 原生行为，不做预检；说明只写"少数特殊正则写法可能需要调整"。生成给客户端的规则原样输出，不受影响。
- M04 接受：镜像装 ca-certificates 与 tzdata；私有 CA 用 SSL_CERT_FILE/SSL_CERT_DIR，说明写明 REQUESTS_CA_BUNDLE 不再生效。
- M07 接受为已知限制：HA 成员永久离线的强制停用在切换后实现；首发说明给人工处理步骤。
- M08：生产切换与公开发布都在 L2–L6 全部跑完之后。
- M11 接受：#40 构建注入唯一版本（提交号），Agent 二进制与镜像同一提交构建。
- M14 推翻建议：autoupdate 镜像**保留**，并改为也重建 Go server（维护者 2026-11-22）。
- M15：缺失 Agent 的日志相关接口改 404，切换前修（进行中）。
- M17 接受：删除"退回 Python 前清理"步骤；停损 = 停止发布、修复前进，或用切换前冷备份重启 Go。
- M18：切换**前**在界面加入域名发现流量文件损坏的恢复入口。
- M19 接受：≥1e6 非整数浮点的指数写法不单列。
