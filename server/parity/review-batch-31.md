# HR 复核 batch-31（主备 DNS 发布自动补完 + 门禁前端阶段）

范围：`go@ded2f9410` 之上 5 个提交（00ac8dcd7 后端自动补完、1c412fda3 前端/文档/截图、4e2f61e9d pyref 元数据、
5d558da67 过时前端测试、e653bc42e gate.sh 前端阶段 + test-gate-routing）。复核者：Opus 5.5，2026-11-22。

## 结论

无高级问题。一处中级问题已修（两次 lease 之间的竞态会把旧内容盖到新的手动发布上），另记若干低风险说明（不改）。

| 级别 | 项 | 处理 |
|---|---|---|
| 中 | `AutoComplete` 在第一把配对 lease 下评估并把旧日志标为 running，释放后再由 `publishPair` 拿第二把 lease 预约。两把之间若有手动主备发布完成（或很快失败），新日志已取代等待意图，但 `publishPair` 不再核对，`reserve` 只看「没有未结束日志」，于是照样预约并把被中断的旧内容发到两台，盖掉用户的新发布 | 修（22d7e0807）：`publishPair` 改收被补完的日志 ID，在预约用的 lease 下重新读取，日志必须仍是它、仍在等待且状态为 running，否则以 `msgJournalChanged` 拒绝；`blockIfCurrent` 发现日志已换就不写 blocked。新增 `TestAutoCompleteYieldsToPublicationBetweenLeases`（evaluate → 手动发布成功 → 旧的 run 被拒、上传数不变、日志仍是手动那次）。负对照：去掉核对后该用例失败（`stale completion: <nil>`，旧内容已上传） |
| 低（说明） | 心跳计数按 `LastHeartbeat` 值变化累加，中间离线（心跳超过 120 s 未更新）不清零：恢复→1 次心跳→再掉线→回来后的第一次心跳即凑满 2 次 | 不改：补完前仍要求两台此刻 online、失联成员 runtime 可读、另一台 runtime 可读，且后续走完整 `PublishPair`（协议、环境、交接检查）；只影响「稳定性」的判定强度。要收紧可在评估时见到成员非 online 就把计数清零（需要 agents 包的时钟钩子才能测） |
| 低（说明） | 补完日志本身再次失联停止时，`markLost` 重新开始等待并以新的停止时间计 24 小时；内容年龄可因链式停止超过 24 小时；同时该日志的 `auto_complete.attempt` 被 waiting 覆盖，历史记录里这一次的「自动补完」标记消失 | 不改：每次都需要一次真实的 10 分钟失联，不会自旋；内容仍是用户最后一次明确发布的内容。若要严格，可在 watch 里继承首次 `stopped_at` |
| 低（说明） | evaluate 置 running 后，若 `publishPair` 在预约前以非用户错误失败且 `blockIfCurrent` 拿不到 lease（或保存失败），日志停在 running，下一轮会再评估并再试 | 不改：此时没有预约、没有下发，重试不违反「只补一次」（只补一次针对已开始的补完发布）；进程在 running 与预约之间重启同理 |
| 低（说明） | evaluate 在 lease 下对新心跳的成员读 runtime（带超时）；期间同时到达的手动发布/停用拿不到 lease 会被拒「忙」 | 不改：与 `advance` 持 lease 的既有模式相同，只在有新心跳时读一次 |
| 低（说明） | gate 前端阶段与 cf-test 共用 `~/cf-test/node_modules/<hash>`；多棵树并发跑 vitest 时，各树的 `frontend/node_modules` 是各自的链接，但都指向同一缓存目录，vitest/vite 的缓存（`node_modules/.vite`）共享写 | 不改：与 cf-test 既有行为相同；该缓存只影响测试排序与预构建，不影响结果。依赖安装与 cf-test-remote 逐行一致（同一 flock 文件、同一 tmp→mv） |

## 逐项核对

### 1. 不该发布时会不会发布

- **停用 / 强制停用**：`Disable` 以日志 keys 拿同一把配对 lease，`dropHA` 清掉两台的 `oxidns_ha_publication`；`awaitingCompletion(nil)` 为假。evaluate 另查 `haEnabled` 两台。停用与 evaluate 同时到达：谁先 TryLock 谁做，另一方本轮跳过（evaluate）或被拒（停用，可重试）。停用落在两把 lease 之间：日志已删，修复后的 `publishPair` 核对失败；修复前 `s.pair` 也会因 HA 已停而拒绝。
- **删除成员**：`DeleteAgent` 在 HA 开启或日志未结束时拒绝（`OxidnsHALocked`），只能先停用，日志随之删除。
- **换配对成员 / 改地址**：`members()` 按日志里保存的 `HAIdentity`（host/port/profile/service_type/enabled）核对，变了即 blocked「主备关系已变化」；`HAPairMatchesModel` 核对配对关系。
- **Agent 重装（同 id）**：地址等身份不变时视为同一成员，这与手动发布一致；补完走完整 `PublishPair`，其中的协议、环境、交接视图检查照常进行，重装后 HA 未就绪会在那里失败（一次，不重试）。
- **两台都刚回来**：停止时两台 runtime 都读不到 → `Lost` 为两台，两台都需 2 次健康心跳；停止时都可读（竞态）也记两台。
- **面板重启**：心跳证据（`heartbeats`/`healthy`）存于日志；等待项让 HA 循环不进入空闲。重启后的 owner 继续计数（`TestAutoCompleteAfterRecovery` 用新 Service 完成第二次）。
- **竞态**：见上表中级项。

### 2. 发布内容

- `readPayload` 要求 payload ID 等于日志 ID，bundle 的 SHA-256 与 payload 元数据一致；payload 目录按成员对共享，但任何新的配对发布都会同时换掉日志，ID 校验保证读到的是被中断那次。
- `childBundle` 只改 manifest 的 AgentID / DeploymentID，文件、ConfigVersion、Comparison 原样；新部署 ID 为 16 字节随机数，`childID` 派生子 ID，不与旧回执冲突（`publishPair` 的「同 did 续跑」分支不会命中）。
- source snapshot / resource / revision / BaseURL / activate 取自旧 payload；面板期间的新配置不进入（`TestAutoCompletePublishesInterruptedContent`：期间改 profile，补完后两台指纹都等于被中断内容，`ConfigVersion` 不等于当前配置）。部署层不以 revision 做冲突拒绝，所以配置变过也能补；之后面板按新 revision 显示待发布，规则自动同步（若开）照常跟进。
- 超过 24 小时：每轮评估先查，blocked。

### 3. 只补一次

- 补完发布的日志带 `attempt=true`，`awaitingCompletion` 排除它；失败即停在 failed（`TestAutoCompleteFailureIsNotRetried`，含之后多轮 Reconcile）。blocked 为终态（`TestAutoCompleteBlockedContent`）。重启后状态从存储恢复，判定相同。
- 「补完途中再次失联」需要一次新的 10 分钟失联超时才重新等待，每次「失联→恢复」最多一次（`TestAutoCompleteOncePerLoss`）；不会自旋。

### 4. 心跳健康

- 计数只在 `LastHeartbeat` 变化时进行；`service_status` 来自经鉴权的心跳。即使心跳被伪造/重放，计为健康还需要面板主动向该 Agent 读 HA runtime 成功（面板→Agent 的鉴权请求），伪造心跳本身不能触发发布。

### 5. 前端

- 行状态：waiting「等待设备恢复后自动完成发布」与 `msgAutoWaiting` 一致；running 行状态为「正在自动完成主备发布」（行标签，后端消息「设备已恢复，正在自动完成本次主备发布」在通知里），blocked 原因来自后端消息原文。
- 轮询：只有 `publishing`、`enableUnknown` 或 `auto_complete.status === 'running'` 时 2 s 一次；新日志出现后按 publishing 继续直到结束，blocked/结束即停止。waiting 不轮询，靠 signature（含 auto 状态）变化重读。

### 6. gate.sh 前端阶段

- 依赖安装与 cf-test-remote 相同（`~/cf-test/node_modules/<lockfile hash>`、`.lock-<hash>` flock、tmp 目录装完再 mv），两台服务器都有 flock/npm；本次在 .64 上 `GATE_ONLY=frontend` 通过，.32 上 cf-test 使用同一布局。
- `run_compare` 中 `(run_frontend) || bad=1`，`fail` 退出子 shell，compare 最终失败；`GATE_ONLY=frontend` 直接失败。路由自测覆盖 typecheck/vitest 失败、compare 包含、core 不含、一份 lockfile 只装一次。
- `rm -rf ../frontend/node_modules` 只删本树的链接（无尾斜杠，不跟随到缓存）；其他树互不影响。
- test-gate-routing：race 次数 3→6 因为每次 core 依次跑 server 与 Agent 模块的 race（ac12f4423 起），夹具补 `backend/agents/go-agent` 目录；期望修改合理。

## 提交

- `22d7e0807` go(dnsha): recheck the stopped journal under the reserving lease before an auto-completion publishes (HR batch-31)

## 验证

见交回消息（.32：dnsha / api/agents `-race`、vet、gofmt、test-gate-routing；.64：dnsha difftest（CONFIGFLOW_DIFF=1）、`GATE_ONLY=frontend`；.32：前端受影响测试文件；preflight）。
