# O14 — N8-7 切换清单补充

此文件记录本轮已经确认的切换影响，不代表完整 O14 演练已经完成。

- [ ] 告知用户：D2 的原生 notification fingerprint 会让仍存在的问题对应的
  已忽略通知重新出现一次。Go 中再次确认后，相同 incident 不会持续重放。
- [ ] RouterSignature 也改变一次。未读旧 router-check 通知可能标为 stale，
  用户下次检查/预览时刷新。调用点仅在 notifications 内，未被规则同步或发布
  状态机使用；不会新增 pending/waiting 任务，也不会自动同步/发布。
  不需要因为签名改变而手动执行同步或发布。
- [ ] 按既定 D2 计划，在 Agent 拉取配置前预热规则缓存；这与本轮通知签名
  的切换相互独立。不要把通知过期标志当作重新发布的依据。
- [ ] 文件格式可以改变空白/键顺序。Python ↔ Go 的 listener、task-status、
  notifications 和 stats_history 文件互通已定向验证。
- [ ] 裸 nonfinite 读为 null，首次重写生成合法 JSON；不是丢弃整个文件。
  原生有效数字（含大整数/指数文本）仍保留原文；绘图溢出点投影为缺失。
- [ ] 紧急切回 Python 前，注意第一批 metrics/traffic 的 null 算术风险：
  calc_stats/get_traffic_stats 可能 TypeError，summary/traffic 返回 500，直到
  24 小时 metrics 保留窗口过期，或手动删去相应 metrics 文件中的 null 点。
- [ ] 本轮 task-status/stats 的 null 计数被 Python 当作零使用。只有手工/损坏的
  notifications occurrences 原来含 nonfinite 时，Go 产生的 null 会让 Python
  下一次不同 evidence 的递增抛 TypeError。通知没有统一的自动清理期限；
  紧急回滚时删除该坏通知项，再由真实来源重新生成，不删除配置/任务文件。
  正常 occurrences 不会生成非有限数。真实 Python 回归锁住此风险。

证据与定向命令：[n8-7b.md](n8-7b.md)。全量门禁与切换演练由协调者/维护者安排。

## N8-9 rules 的 O12 发布说明输入（2026-11-19）

Go使用D2原生规则缓存摘要。旧缓存参与生成时的离线兜底，因此切换后不能仅按新文件名
查找：当前存储声明和内容校验均通过时，兼容读取旧文件并迁入新文件名。迁移保持旧mtime，
不延长刷新间隔，不改数据库revision；目标已有文件时不覆盖，旧文件保留供回滚。
只读快照或迁移写失败时仍返回已验证旧内容，下一次可写读取重试迁移；invalid receipt、
拟议URL/behavior变更和不匹配声明均拒绝借用旧缓存。离线上游下生成不会因此丢掉有效旧规则集。
详见 `progress/n8-9-rules.md` 的真实Runtime、离线迁移和负控验证。

## L2 发布阻断：Docker Agent 镜像与面板版本配套（2026-11-21）

实验网 L2 发现：Go 面板生成的 Docker Agent 命令配 Docker Hub 上旧的 `config-flow-agent:latest`（入口脚本不写
`AGENT_<SVC>_REGISTRATION_TOKEN`）时，开启登录后新装必然 403。修复见 `go(image): pair Docker Agent image with panel version`，
差异登记见 behaviour-changes「O9-H2 发布补充」，发布顺序见 o14-switch-plan P13、B.2.1，O12 U16。

合成验证（.64，Docker 29.9.3；全部用合成名称、自建 bridge 网络 172.30.77.0/24，结束后已删除容器、网络、镜像、数据目录）：

| 场景 | 面板 | Agent 镜像 | 结果 |
|---|---|---|---|
| 新令牌 | 当前树 Go 面板（开启登录，`-X version.AgentImage=cf-agent-pair:test`），未改动的 `docker-agent-run` 脚本 | 当前树 `Dockerfile.agent` 构建 | 命令/Compose 的镜像均为 `cf-agent-pair:test`；注册 200，Agent 在线 1.8.61-go，配置里只剩 `agent_id`/`token`（凭据已清） |
| 过期令牌 | 同上；停面板后把该会话 `expires` 改为过去时间再启动 | 同上 | 注册 403「安装命令已失效，请在面板重新生成安装命令或设置注册密钥。」，未建 Agent |
| L2 复现（对照） | 同上，新令牌 | Docker Hub 旧 `latest`（2026-11-14 构建，入口无 `REGISTRATION_TOKEN`） | 403 同上文案 |
| 新镜像 + 旧 Python 面板新装 | `main`@5cd721957 Python 面板（开启登录），它生成的命令（无凭据变量）只把镜像换成新构建 | 当前树构建 | 注册 200，在线，心跳正常；Python 日志无错误 |
| 旧镜像 → 新镜像原地升级 | 同上 Python 面板 | 先用旧 `latest` 安装上线，再用同一数据目录换新镜像重建 | 「Agent already registered」沿用原 ID，心跳成功，Python 面板显示在线 |

兼容性结论：新入口脚本在变量缺失时写入空 `registration_token`/`registration_key`，Agent 发注册请求时按 `omitempty` 省略；
心跳 `host` 只在面板声明 `reads_host` 后发送，因此旧 Python 面板看到的请求与旧镜像一致，无需修改入口脚本。

附带发现（未修，交协调者）：`main` 上的 Agent 与 `go` 分支的 Agent 都报 `1.8.61-go`，但只有后者支持一次性注册凭据；
同一版本号对应两种能力，升级判断和排障会混淆。建议发布前在 `go` 分支提升 Agent 版本号。
