# O12：Go 切换行为差异与升级说明

本文是发布前的差异台账，供 O14 切换计划和公开升级说明复用。资料快照：2026-11-19，`go@bcbb6e573`。只汇总已有决定，不新增产品决定，不代表 O14 演练、实验网验收或最终发布门禁已经完成。

## 范围、去重与判定

- 核对范围：本目录的 [behaviour-changes.md](behaviour-changes.md)、`server/` 中所有 `review-*.md`、`parity/progress/*.md`、`PROGRESS-*.md`、含 `HANDOFF` 的交接文件，以及 [O14-CHECKLIST.md](O14-CHECKLIST.md)。185 份复核/进度/交接文件中的同一决定只登记一次；补充说明、复核和回归共同作为来源。
- A 是受影响部署必须执行的操作，逐条列出做法、时间和不执行的后果；B 是可见行为变化，正常面板用户无需执行统一迁移；C 是实现、文件或诊断层差异，用户无须处理。特殊输入的修正建议不等于要求所有用户改配置。
- 同一事项采用较新的明确裁定。历史 `jsonx`/PyYAML 模拟规则由 N7/N8 原生规则覆盖；共享 String/Int 规则采用 N8-8a 最终裁定及 batch-6 补充。不能把所有 JSON 字段概括成“只接受原始字符串”。
- 已修复的 Go 移植错误不是差异；仍缺 owner、未做实验验证、仅在测试比较器中的时间/随机 ID 归一化，也不是永久产品差异。资料冲突、尚未证明影响面的项目单列为“需要维护者确认”。
- 下文“来源”均为仓库相对链接，节名或问题号用于定位；“内部错误文本原生化”等共同类别只登记一次，模块细节在该条列明。

## A）切换后需要操作的事项

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

### A20　仅在紧急退回 Python 时清理不兼容的 null 历史点

**怎么做：** 常规升级保留历史，不做清理。若维护者决定紧急退回 Python，先备份，再从受影响 Agent metrics JSON/JSONL 去掉 nonfinite 修复形成的 null 点，或等待 24 小时保留窗过去；若损坏的通知 occurrences 从 nonfinite 变成 null，删除对应坏通知项，再由真实来源重新生成，不删除配置或任务文件。

**什么时候：** 仅紧急回退前，属于回退预案，不是常规升级步骤。不处理时 Python 可加载/保存文件，但 summary/traffic 算术或通知递增可 TypeError/500；通知无统一自动过期时间，不能靠等 24 小时解决。task-status/stats 的 null 计数可由 Python 当零使用；Go 可直接处理缺失值。新极大整数 JSONL 点还可能被 Python 的 4300 位限制跳过。

来源：[差异表：N8-7 两批回退注意](behaviour-changes.md)、[O14 补充](progress/o14-switch.md)、[N8-7 文件交回](progress/n8-7.md)、[N8-7b 复核](review-n8-7b.md)。

## B）用户可见，但正常使用无需统一迁移的变化

### HTTP、认证和安全出口

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
| B09 | 规则下载/缓存刷新逐解压层及最终正文均限 64 MiB，不受压缩体积、Content-Length、streaming 绕过；更小的部署文件限制仍生效。超限拒绝且不缓存部分内容，保留上一份验证内容。Sub-Store 专属路径范围见 M06。 | [差异表 batch-8 O9-M2](behaviour-changes.md) |
| B10 | 内部规则 URL 在原始分类、规范化及每次 redirect 都守住 origin/API 边界；歧义 dot/编码/反斜杠逃逸更早 400，IDNA/punycode/全角面板域名也按内部处理，禁止目标零请求。合法内部转向、外部来源、名称中的普通反斜杠保留；无 host 的 `http:ok` 按原生 URL 规则拒绝。 | [差异表 CFG-01](behaviour-changes.md)、[安全复核](review-cfg-c14-security.md) |

### 请求类型、存储容错和诊断

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
| B20 | 其他 typed 消费使用 schema 缺省或明确错误，保留原始存储：错误形状的 interval/health_check/custom text/规则列表、EasyTier 根/生成节/version、discovery 身份、health/refresh 回执等不再进入 Python 动态 truthiness/容器遍历。类型错误 enabled 的行为按所属 schema；不概括成一律关闭。损坏 rule library 不能证明 IP presence 时返回 unknown，避免借零值读取缓存。 | [差异表 N8-8a、8b、N8-9 health/rules](behaviour-changes.md) |
| B21 | 数据库打开只查结构，名称/revision/引用/Agent 绑定等业务错误推迟到写入；错型值可容错读并留原字节，修复前无关写入仍被校验拒绝。无效业务值不触发启动 token 维护写入，避免 Python 整体打不开或误改坏数据。 | [差异表 N8-5a](behaviour-changes.md)、[N8-5a 复核](review-n8-5a.md) |
| B22 | 三方合并按 JSON 类型区分 `1`、`1.0`、`true`、`0`/`false`；修订号接受整数值的浮点写法，仍拒绝负数/字符串/null/缺失，bool 仍沿原写入合同。路由连接保护也认类型变化，畸形发布记录仍阻止删除/改地址。名称错型用 JSON 表示；被引用资源名称坏类型时显示 ID。 | [差异表 N8-5a/b/c、C34 false/0](behaviour-changes.md)、[N8-5 交回](progress/n8-5.md) |
| B23 | 共享节、domain-discovery 设置/历史、stats_history 根等未知成员在更新中保留，修复 Python 丢字段。旧格式导入的部分错误 shape 从内部500变明确400或可正常转换。 | [差异表 N8-5b/c、Step B R058-B03、N8-7b](behaviour-changes.md)、[PROGRESS-config C37](PROGRESS-config.md) |
| B24 | 普通 JSON 响应的 NaN/±Infinity 输出 null，图表视为缺失。旧 metrics/traffic/task-status/notifications/stats 文件按值修复 nonfinite，保留有限兄弟值/数字原文，首次重写为合法 JSON；summary 跳过缺失/不可表示样本，全缺失为null，流量投影跳过坏计数、保留 raw。超大统计点不再因绘图 float 溢出500；原生数字范围可超过 Python 4300位及float64。文件/stream 透传例外不改变，回退风险见 A20。 | [差异表 N8-1、N8-7 两批](behaviour-changes.md)、[N8-7b 复核](review-n8-7b.md) |
| B25 | CPython-only 字符规则改用 Go Unicode/ASCII 语法：部分 C0 首尾字符不再 trim；任务计数拒绝非 ASCII 十进制数字，但合法 ASCII 计数中的符号/单个数字分隔下划线仍按合同读取；监听/sniffer 端口使用原生 ASCII 解析，`CONFIGFLOW_NODE_TEST_TIMEOUT_MS` 不接受下划线/Unicode数字。provider 改名中的非 ASCII `$n` 保留字面量，down 非 ASCII 数字不作带宽。日志保留 Unicode 新增可打印字符，仍过滤控制/换行/bidi。MosDNS 首尾 U+001F 可进入规则文本，手写此类规则应移除控制字符；普通空格/合法 Unicode 空白不变。 | [差异表 N8-7/8a/8b/8c、N8-9 health、MosDNS 空白](behaviour-changes.md)、[C07复核F1](review-c07.md)、[PROGRESS-config 原生规则](PROGRESS-config.md) |
| B26 | 正则直接用 RE2/regexp2：word boundary/`$`/空串 `\B` 依各引擎；部分 variable-width lookbehind、非起始 flags 等可用；超 int32 repeat 不支持，回溯预算1秒后不匹配。规则文件只按 LF/CRLF/CR 分行，VT/FF/NEL/U+2028/U+2029 等不是 Python 式分隔符。`\N{name}` 的残余行为与 Unicode/IDNA 少见输入见 M01。 | [PROGRESS-config 原生 rules 决定](PROGRESS-config.md)、[review-c19-c20](review-c19-c20.md) |
| B27 | 原生错误原因替代 Python exception/repr：保留已约定中文上下文与业务状态，YAML 增加可用的一基行列；多文档/无类型名标签有中文位置提示。错误原值以 `null/true/[...]` 代替 `None/True/repr`，设备多字段不一致按排序后的首字段提示。health worker 非法 UTF-8 固定中文，不回显 worker 内容；R13 意外读取失败用「读取失败：内部错误」。 | [差异表 N7-2/3d、N8、A23、R13](behaviour-changes.md)、[R04](review-r04.md)、[R08](review-r08.md)、[A11](review-a11.md) |

### YAML、生成和缓存可靠性

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

### 发布、设备、安装和运行状态

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
| B62 | health评分delay用float64，超范围按R5而非Python任意精度运算；worker正整数latency继续用json.Number保大数精度，失败error须非空文本，不接受truthy容器。请求的健康检查参数、worker version/latency仍严格按协议校验，不因存储兼容读取放行。 | [差异表 N8-9 health/check](behaviour-changes.md)、[health交回：明确边界](progress/n8-9-health.md) |

## C）内部行为，正常用户无须处理

| 编号 | 有意差异与边界 | 来源 |
|---|---|---|
| C01 | Python/Werkzeug/Nginx运行与调优模块改为 Go 原生；Go 服务不依赖 Python 树，schema/template/script等嵌入。Go 在启用 CONFIGFLOW_FRONTEND_DIR 的完整部署中监听并服务 SPA；没有 Nginx 配置文件/重载进程，但换端口失败应保旧配置和可达性。API-only 模式不承诺 Web listener。镜像部署确认见 M04。 | [总交接 Not ported](../HANDOFF.md)、[差异表 Web监听](behaviour-changes.md)、[PROGRESS-config 决定16](PROGRESS-config.md) |
| C02 | 资源锁改进程内锁，不创建 Python task-status/notifications/web-listener/deployment/dns-monitor/rules-cache/profile-operation/Sub-Store work/control/agent-rule-sync worker 等 .lock；旧内容/inode/mtime保留，缓存清理也不删旁锁。实例锁、兼容调度器生命周期锁、.config.lock及目标bootstrap的.compose-enrollment.lock保留。 | [差异表 O12锁文件](behaviour-changes.md)、[O3-6 交回](progress/o3-6-locks.md) |
| C03 | Runtime统一跟踪后台工作/锁/取消：浏览器断连不取消已受理设备批次（RouterOS从旧100/150改完成150/150）；真实关机停止后续I/O/提交。SQLite、leaf文件最终提交检查同generation，原子提交完成后不再因晚取消误报失败。用户停机步骤见A02。 | [差异表 O3-6b](behaviour-changes.md)、[O3-6c](progress/o3-6c-shutdown.md)、[O3-7](progress/o3-7-concurrency-gate.md) |
| C04 | D2内部发布/策略/归属/connection/reminder缓存/preview摘要采用稳定排序原生JSON；不追Python空白/转义/数字拼写。对外操作影响已逐项在A登记；R13缓存不落盘无迁移/TTL，未知继续重试；A16防抖state.key一次自动重建，先查询旧已接受任务，不提前接受源。 | [差异表 R12/R13/A16/R15](behaviour-changes.md)、[R08复核](review-r08.md) |
| C05 | tar/gzip/ZIP与文件JSON空白/键顺序、YAML布局会变，语义/成员顺序在有序处保留。归档SHA可变；内容v1 Fingerprint对未变真实文件仍沿固定编码，不是所有摘要都变。订阅health/transport/nodes缓存JSON完整值仍相同。 | [差异表 A11、N7-4、N8-9rules](behaviour-changes.md)、[C35交接](C35-HANDOFF.md) |
| C06 | typed DTO/错误链替代动态dict/模拟异常，读取与生成不偷改调用方工作副本（VLESS encryption/reality short-id在独立输出补齐）。窄codec只解消费字段，不保留无关瞬态func/channel碰巧导致Marshal失败的行为。 | [差异表 N8-8a输入不变、batch-6codec](behaviour-changes.md)、[batch6日志/性能交回](progress/batch6-logs-zh.md) |
| C07 | matcher/proxy YAML缓存预算按Go内存估计，不模拟sys.getsizeof；只影响缓存准入，不拒绝输入/改变结果。目录清理结果可排序而非目录遍历顺序；临时文件名、警告先后、缓存slot绝对路径或时钟起点等内部细节可变。 | [N7-3b复核](review-n7-3b.md)、[PROGRESS-config C09/C12/C13](PROGRESS-config.md)、[PROGRESS-agents A04](PROGRESS-agents.md) |
| C08 | SQLite连接及时关闭、checkpoint，`-wal`/`-shm`存在时间不同于Python GC；实际数据/schema/journal mode相同。WebDAV JSON内存编码，不创建Python临时文件；非法JSON在PUT前拒绝。 | [PROGRESS-agents A07](PROGRESS-agents.md)、[C35交接](C35-HANDOFF.md) |
| C09 | 原生Go日志格式/OS/网络/SQLite原因替代Python repr/errno/traceback；多无效项按文档顺序或排序键报首项，避免set随机顺序。移除含敏感渲染内容的Surge DEBUG和Mihomo重复ERROR；只记一次安全错误。HTTP已断开且错误等于请求取消cause时不误报Exception，真实无关/后台失败仍记。 | [差异表 N8-7、batch-6日志](behaviour-changes.md)、[C18复核](review-c18.md)、[C19/C20复核](review-c19-c20.md)、[PROGRESS-config C03/C09](PROGRESS-config.md) |
| C10 | 内部能力头、MCP/config/注册凭据比较用常量时间处理，公开日志URL/Referer脱敏；不改真实凭据或有效请求协议。HTTP compact JSON替代requests带空格body，设备含义/既定业务大小计量仍按原合同。 | [差异表 O9-L1/L3、MCP](behaviour-changes.md)、[R04](review-r04.md)、[PROGRESS-routers R05](PROGRESS-routers.md) |
| C11 | R11 ND DNS列表stable快照用JSON而非repr；R14私有_rule_order为顺序索引，公开rules仍有序相等，缺省零值字段可显式存在。R12原生error由调用方统一补偿，无Python ReleaseError类。 | [差异表 R11/R12/R15](behaviour-changes.md) |
| C12 | H2新增system.agent_registration_sessions typed字段、仅主库存hash/身份/expiry，repo事务内签发/消费；旧库缺字段可读、不改schema version，备份不恢复会话，下载脚本仍含临时明文（A15）。format=both是Go新增可选JSON compose/run双输出，原无参数文本API保留。 | [差异表 H2 D2/R2–R4](behaviour-changes.md)、[H2交回](progress/o9-h2.md) |
| C13 | 无调用者的models、subscription_parser.parse_subscription/parse_clash_yaml、Surge get_aggregation_nodes及旧运行调优不移植；功能入口保留。未发生掉功能的测试helper替换、replay拆分、随机值归一化、fuzz/gate并行均不列产品差异。 | [总交接 Not ported](../HANDOFF.md)、[PROGRESS-config Known differences/决定12](PROGRESS-config.md)、[C19/C20复核](review-c19-c20.md) |
| C14 | artifact生成一次捕获配置与revision，避免Python生成后才读revision、并发保存导致内容/版本错配；冻结内容仍可在之后改草稿/切默认空间后读取。该修正在复核中标为“Python bug，Go已修，非差异项”，这里登记内部一致性决定，不据此要求用户迁移。 | [artifact复核](review-artifacts.md)、[artifact交回](progress/artifacts.md) |

## 需要维护者确认的条目

这些条目不是批准的功能缺失，也不应不加限定直接写入公开升级说明。已确定部分已在 A/B/C 登记；这里仅列待确认的影响面或最终状态。

| 编号 | 待确认内容与原因 | 维护者应确认什么 | 来源 |
|---|---|---|---|
| M01 | 原生regex/IDNA/IPv6/Unicode残余：`\N{name}`在通用过滤验证可被当unsupported/no-op保存，preview会列全部候选；provider路径另走明确拒绝，超int32 repeat也拒绝。İ/ı/ſ大小写、IDNA2003专有名、带scope的network、IPv4-mapped文本等少见输入可能不同。早期“repeat验证也放行”等记录已被新裁定覆盖。 | 是否有用户保存这类过滤/改名/域名；需要时提供逐配置修正方法，再决定是否追加A类。不能笼统宣传“正则全兼容”。 | [PROGRESS-config 原生rules决定与C12](PROGRESS-config.md)、[N8-8a交回](progress/n8-8a.md) |
| M02 | CORS变更确定，但独立域名前端、第三方网页管理API等部署数量未知；CORS是浏览器读取许可，不能代替服务端认证/拒绝执行。 | 是否发布同源部署调整指引；受影响者应使用同源面板/反代路径。不要写成“跨域请求一律403”。 | [O9修复H1](progress/o9-fixes-1.md)、[O9复核H1](review-o9-fixes.md) |
| M03 | 共享数值文本、bool严格化、超大数/非ASCII数字等对手工库/导入文件的实际影响未知。具体例如数字密码token写法、数值GitHub代理域名5、discovery auto_probe:1、typed缺省会改变消费结果；A19已登记明确的DNS修复要求。 | 是否需要只读预检/逐项用户诊断；不能要求用户手工批量转换整库，更不能把所有ID/name数值都列为拒绝。 | [差异表N8-8a最终裁定、N8-9discovery/DNS](behaviour-changes.md)、[batch6 R5](progress/batch6-r5-fix-zh.md) |
| M04 | 原生HTTP取代Nginx、TLS根改用系统CA（旧requests用certifi/部分环境变量）、时区及嵌入资源依赖属于镜像/运行部署合同。历史资料提示镜像需ca-certificates/tzdata，完整Nginx+Flask对照和Web切换仍归O9/O14实验。 | 最终镜像是否齐全、私有CA/REQUESTS_CA_BUNDLE依赖如何部署、对外端口/反代路径是否完成演练；确定后补实际运维操作，不臆造新环境变量。 | [R04复核TLS](review-r04.md)、[PROGRESS-config C13/C14/决定16](PROGRESS-config.md)、[差异表Web监听](behaviour-changes.md) |
| M05 | 早期send_file有条件/Range/ETag/日期解析差异，文档同时要求C06修复；L1 D7只批准不可满足范围500→416，不能将所有历史偏差一概接受。 | 逐项确认超大Range、Latin-1空白、If-Range/If-Match/304头/日期格式最终闭环；未关闭项继续作缺陷，不作发布豁免。 | [PROGRESS-config 决定15](PROGRESS-config.md)、[PROGRESS-agents A10](PROGRESS-agents.md)、[L1复核](review-l1-installer.md) |
| M06 | 旧Agent HTTP/Sub-Store/rulefetch资料记载.netrc不读取、请求默认头/redirect跳数/解压/编码猜测差异；charset_normalizer、BOM-less UTF16/32、系统CA、非UTF8替换粒度及`;params`/坏query等边界有早期接受记录。N2代理已覆盖旧loopback决定；N8-9 subs review尚待后续批次，64MiB Sub-Store专属解压限制不能仅凭共享rulefetch结论宣称全覆盖。 | 最终N8-9 subs/Agent原生迁移后哪些仍成立、真实上游/部署是否依赖；按最终交回补条目。异常token头控制字节、非标准压缩/深JSON等Go Agent不产生的输入与正常协议分开。 | [PROGRESS-config C13/C14/C29](PROGRESS-config.md)、[PROGRESS-agents A01/A06/A18](PROGRESS-agents.md)、[总交接batch-9候选](../HANDOFF.md) |
| M07 | A18早期O-1：成员永久失联会卡unknown；后续总交接已决定离线10分钟后强制停用，但本范围未给出最终实现/公开操作闭环。 | 是否已实现、具体“离线10分钟”的判定及UI步骤、L3连续性/遗留清理是否验收。先保留B48已明确锁忙重试决定，不把新强停写成已可用。 | [A18复核O-1](review-a18.md)、[A18交回](progress/A18-handoff-zh.md)、[总交接2026-11-18 O-1决定](../HANDOFF.md) |
| M08 | RouterOS/iKuai归属和补偿已批准安全收紧，真实固件的ID复用/console脚本结果、netmask后缀、PD晚写状态、断链/关机遗留仍有L5项目；R03还记录不可达的index耗尽残余、超长单项、未知op及尾换行归属边界。 | L2–L6（尤其L3/L5）哪些完成、现场失败后的人工核对步骤；不得将未做实验等同功能差异，也不得把共有的未修Python bug当Go改进。 | [R04–R11复核](review-r04.md)、[R05复核](review-r05.md)、[R09复核](review-r09.md)、[R11复核](review-r11.md)、[PROGRESS-routers R03](PROGRESS-routers.md) |
| M09 | C07“任务slot未齐列表503”、C34 artifact503、A21/C36缺owner、A14共享commit skip等多为历史中间状态；HANDOFF已记全路由接线，A21/部分共享窗口有明确关闭记录。 | 发布树确认没有残余deferred owner/占位/skip，清掉过期比较豁免。不得把这些503列成长期B类变化；若真实发布树仍返回则继续修。 | [差异表C07/C34/A21](behaviour-changes.md)、[A14交接](A14-HANDOFF.md)、[O3-6c](progress/o3-6c-shutdown.md)、[artifact复核](review-artifacts.md)、[总交接batch-8](../HANDOFF.md) |
| M10 | 早期手工/损坏数据边界有额外接受记录：SQLite超int64 generation/非整数position、清理unlink失败忽略、copy不复制xattrs/flags、metrics目录初始化由启动错误改成日志/保存错误、异常Agent ID路径留在metrics目录、极端时间/计数/NaN排序差异；旧jsonx孤立surrogate/递归阈值多已被原生N8覆盖。 | 哪些在最后迁移后仍可达、是否需修复/备份修复说明；原生资源上限按最终包确认，不能照搬旧10000/490等阈值或早期“无损模拟”宣称。 | [PROGRESS-config C01/C02/C03/C15/C16](PROGRESS-config.md)、[PROGRESS-agents A04/A05/A08/A09/A10](PROGRESS-agents.md) |
| M11 | A11 RulesFingerprint依赖构建版本；不同validator代码若都以dev版本构建会共享标识。安装器嵌入probe不再有运行时文件缺失503，GeneratorFingerprint常量亦靠漂移校验。 | 正式发布是否正确注入唯一版本/构建标识，并同时发布配套Agent二进制/镜像；确认后A05/A14的证明与首次注册要求才有部署保证。 | [A11复核：Accepted debt](review-a11.md)、[PROGRESS-agents A08/A10/A11](PROGRESS-agents.md)、[H2交回](progress/o9-h2.md) |
| M12 | MCP在多步工具中可能部分完成后返回-32603；已批准Runtime边界，但真实MCP客户端和设备操作仍待发布层验收。 | 是否增加公开“报错后先查询再重试”说明，是否另做部分完成业务回执；本O12不改变工具事务设计。 | [MCP复核L5及后续项](review-mcp.md) |
| M13 | N8-1明确只修普通响应nonfinite→null，A04/A05新上报点的拒绝/丢弃另作后续；N8-7读文件修复也不等于所有Agent ingestion都已迁移。当前总交接仍列N8-10 Agent core等在途。 | 最终Agent写入边界是否仍允许产生nonfinite、升级后历史保留/图表提示是否需另补说明。发布说明不能把“旧文件按值修复”写成“所有新上报已拒绝”。 | [差异表N8-1及N8-6](behaviour-changes.md)、[总交接N8-10](../HANDOFF.md) |

## 来源冲突与已撤销记录的处理

- A08/A09旧surrogate、异常Host500、非字符串session token等历史接受项不直接宣称仍是正式合同，随最终原生边界归M06/M10；server-domain/config-token孤立surrogate明确拒绝则保留B12。
- C34曾考虑统一null/缺失为无更新，独立复核已撤销，采用B13逐引擎真实合同；`config:false`→`config:0`仍是N8-5类型合并差异，不恢复Python相等规则。
- O9复核R2最初描述凭据满额503，后来已以format=both共享凭据并淘汰最早未用项修复；采用A14，不再提示“等待24小时才能生成”。R3–R6的文档/警告/固定IP补充也以最终追加为准。
- R04早期“ApplyMany批次间响应浏览器取消”由O3-6b最终Detach决定覆盖；真实Runtime关闭仍取消，不扩大R12的本地记录特例。
- A21缺C36 owner503、artifact deferred、原N8-9 rules“实施中”、A14共享commit窗口、CFG-08依赖未接线等历史进度不作为永久差异；合并树确认归M09。
- 测试副本格式转换、随机ID/时间与端口逐端验证、生产比较器中的精确baseline映射只是验证办法。不得在生产改Fingerprint、清changed或丢弃完整字段来模拟“无差异”。

## 面向公开用户的升级说明草稿（中文）

> 发布前由维护者补入版本号、升级窗口，并确认上面的待确认项；以下不表示现在已经发布。

本次升级将 ConfigFlow 后端从 Python 切换为 Go。已有 SQLite 数据继续使用，前端和已有 Agent 的日常功能保留。配置排版和部分校验会变化，请按以下步骤完成首次切换。

1. 如果仍在较旧版本，先备份 DATA_DIR，升级到最后一个 Python 版本并成功启动一次，完成旧数据迁移。切换时等正在进行的设备操作结束，停掉旧服务，同一数据目录只运行一个新服务。
2. 在部署配置里保存随机、至少32字节的正式 `JWT_SECRET_KEY`，不要使用示例值。首次更换后重新登录、重做未确认的固定IP预览；不保存正式值会使这些令牌每次重启失效，已经生效的固定IP不受影响。
3. 保留旧规则缓存，在Agent首次拉取前预热/核验规则，再逐台全量推送一次。新YAML排版和缓存标识会造成一次 `changed`，请确认核心加载和部署成功，最后检查显示 `synced`。旧OxiDNS ready候选也需重新推送校验。
4. 使用IPv6的用户重新检查并做一次验收；DNS跟随旧绑定重新选择一次；路由自动规则同步若暂停，请手动预览发布一次建立新基准。重新出现的已忽略通知检查后再次确认即可，不需要仅因通知变化重新发布。
5. 切换前打开的编辑页/预览请重新载入。若收到规则库409或固定IP预览失效提示，重新读取/预览后再确认。少数含特殊Unicode上游标识的原生配置需按当前上游重新生成分流。
6. 新装Agent请用新面板生成的安装命令及本次配套程序/镜像。开启登录后，无注册授权的手拼命令不再可用；一次性命令24小时内有效，也可能因生成过多新命令提前失效，请保密并尽快执行。已有Agent使用自身token，无须仅为此次注册调整强制升级。

如果强制DNS检查提示未登记的 `cf-dnsguard:hijack` 规则，请核实外部规则并改用非保留注释，再重新预览。不要仅按注释批量删除或接管设备规则。

新版本还修复了HEAD误写、缓存并发覆盖、安装失败恢复、设备归属核验和凭据泄露等问题；非法输入通常会得到更明确的400/415/409，普通面板操作无需额外迁移。跨域浏览器调用不再获得任意来源许可，请使用同源面板；后端请求体及规则解压内容有统一上限。使用WebDAV点目录、环境代理特殊写法或错误类型OxiDNS动态设置的部署，请按完整升级台账修正后再执行相关任务。
