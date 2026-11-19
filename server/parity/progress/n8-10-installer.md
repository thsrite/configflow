# N8-10 installer S pass

状态：进行中，未交回；不能视为 verified。

工作分支 `go-n8-10-installer`，初始基点 core-2 `a75dd4a1d`。
已完成原生模型主体：单服务/AIO 配置 DTO、Compose service/mount/environment/network 模型、
HA 四种生成出口/迁移命令、preflight report、IssueCapability/EnrollIdentity 原生凭据事务。
旧调用方目前通过独立 legacy codec 接线；不会复制业务。

已编译检查（.32）：`rt -- bash -lc 'cd server && go test -run "^$" ./internal/installer ./internal/api/agents ./internal/agents/upgrades'`，exit 0。
中间定向语料揭示的剩余工作：批准的人工 preflight 字符边界与损坏会话旧行为须绑定独立
Python/native 期望；JSON 格式差异仅解析对应 JSON 后比较；最终 race/vet/difftest/fuzz 未执行。
原 corpus / pyref entries / baseline 未修改。

## 已批准裁定

- 会话 index/session.id 均为 32 位小写 hex 且相同，否则 issue409/enroll401、无写入。
  Python 唯一合法创建入口 `backend/agents/docker_ha_enrollment.py:58` 为 `uuid.uuid4().hex`。
  .32 离线快照仅计数：会话 0，非规范 ID 0；未复制内容。
- preflight 机器序号与 ss 计数仅 ASCII，原生 strings.Fields/TrimSpace；异常须 unknown。
- `identityJSON` 为**保留的 D2 身份编码，非 legacy facade**：仅安装 spec index 与 Compose
  owner/project，保持 sorted-key ASCII JSON 和既有分隔空格；无 jsonx/pystr。以后统一
  D2 owner 时迁移。普通 config/request/response 使用原生 JSON DTO。

## 对外接口（待最终核对）

- `GenerateGuideJSON(json.RawMessage, *Enrollment) (*GuideResult, error)`：exact-key DTO。
- `IssueCapability(ctx, dataDir, json.RawMessage, profileID) (*Capability, error)`。
- `EnrollIdentity(ctx, dataDir, AgentEditor, sessionID, token, present) (AgentIdentity, error)`：
  AgentEditor 使用 core-2 `EditAgents`，原始字符串身份核验与同事务重放。
- 旧 `GenerateGuide` / `GenerateCompact` / `GenerateDirect` / `GenerateBundle` / `Enroll` /
  `IssueEnrollment` 只是邻居 codec；待消费方 S pass 后删除。

剩余：完成旧 consumer 必要接线、原生标量入口收口、独立期望/安全回归/最终定向验证；
在方便检查点 rebase 到 fresh HR `70cea1eb6`，然后继续。
