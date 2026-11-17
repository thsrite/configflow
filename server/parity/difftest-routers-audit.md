# Routers Step B 可达性审计记录

状态：进行中，等待逐条补齐及独立复核。这里的提议不等于矩阵条目已完成。
只依据 Python、真实公开进程及合成持久数据；未读取 Go 业务实现。

## R002-B02：省略 profile_id 的旧 Agent

提议：把“运行时缺字段回退”与“旧格式导入迁移后读取”分开。不能把缺字段
Agent 直接装入正常 HTTP 成功夹具，再声称验证了运行时回退。

- `backend/common/config_repository.py:2390`：旧格式导入会补 `profile_id=default`。
- `backend/common/config_repository.py:2487`：当前文档要求每个 Agent 的
  `profile_id` 存在于 profiles 中，缺失或空绑定不符合要求。
- `backend/common/config_repository.py:897`：公开进程首次读取存储文档时也执行该校验。
- `backend/routes/client_devices.py:49`：运行时 `agent.get('profile_id') or default`
  回退位于上述存储加载校验之后，不能通过修改导入夹具绕过前置条件。

实际边界证据（测试服务器 .32）：

1. `test_seed.SeedTests.test_missing_agent_profile_does_not_reach_public_http_fixture`
   调用真实 Python 公开 import API；缺字段 Agent 被 `ProfileValidationError` 拒绝，
   提示“绑定的配置不存在”。没有启动 HTTP handler，不计为 HTTP 覆盖。
2. `test_boundary.BoundaryTests.test_stored_legacy_missing_profile_is_rejected_before_handlers`
   先公开导入合法合成库，再停止 Python 公开进程；仅在该独立 DATA_DIR 的 SQLite
   行中删除一个 Agent 的 profile_id，重新执行真实 `python -m backend.app`。
   进程启动失败，日志包含相同校验提示。没有替换仓库、AgentManager 或路由函数。

合法 alpha/beta Agent 的 query 固定空间、响应头保留请求空间、失效 header 优先
返回 404、HEAD 忽略 query agent 等属于独立可达分支，已经另写 HTTP 配方。
旧格式迁移后的 Agent 归属仍需补一份真实公开导入配方；本条保持待审计，不能标完成。

## R012：被前置 blocker 拦下的计划

`backend/routers/plan.py:527` 在未关联网关或其他早期 blocker 成立时返回空 stages、
空 script、空 fingerprint，并且 empty=true。该返回不是可确认发布的有效指纹。
`unlinked_inspection` 明确检查三个空值及 blocker；不能拿十六位 hash 的正常成功
约束套在这个分支上，也不能把 empty=true 当作已同步或可以发布。
