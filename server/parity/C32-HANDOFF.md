# C32 规则路由（进行中）

分支 go-port-c32，从 go d3124353 建立，A24 分支已冻结不动。范围为 PROGRESS-config C32 的全部 21 个方法。Python backend/routes/rules.py 已完整阅读；验收必须覆盖每条路由成功和失败、实际 DB/文件、真实 Python、注册冲突、fuzz 和 proddiff。

已提交 ed7f5f26：coordinator 批准的 agents.Hooks.PinnedWithheld(ctx, profileID, ruleIDs)(bool,error)，由 A16 注册。无自动同步 OxiDNS 时确定 false；需投影而未接线/计算失败时只有一条不含内容/URL 的 warning、省略该字段、已提交保存不回滚。A16 和 C32 PROGRESS 已明确 deferred → A16。不得大范围忽略其他差异。

847da438：首批16方法，server 已接线。native 稀疏 JSON patch/响应类型、CRUD、batch、reorder、group-name、policy、GET/PUT dns、owner lifecycle.Detach，C09/C27 逻辑不复制。JSONx 仅 adapter。hook nil/error/true/false/no-device、持久化不回滚和日志不泄漏测试通过。全应用注册测试通过。

当前 compose 实现与测试已编译通过，17/21。复用共享校验、AllowedCommaPayload getter；保持逗号 payload；服务器生成ID；共享库+profile+DNS同一事务；预验证远程源；提交后缓存失败返回警告。原子回滚、取消、并发去重及纯解析fuzz种子测试通过。

下一步：
1. 完成两条公开 local provider；复用 rulefetch/refresh 的缓存、2秒下载、stale、Mihomo delivery-only normalization、422/502及镜像语义。
2. match-test 和 find-duplicates，直接用共享 RuleConfigMatcher；查重用紧凑 typed projection，避免把已 hydrate 的整个 profile JSON 重复物化和为每个命中造 map。
3. 真实 Flask oracle + HTTP 差分（全21方法、合成数据/回环fakes）；每步比较 DB/所有文件；映射7个Python测试文件；HEAD需保持只读，记录Python HEAD DNS落入写分支的bug修正。
4. 检查C27 GET read-model的base_url与legacy Agent名称字段是否需要最小共享修正，先以真实差分验证。
5. 补注册/并发/取消/D1 duplicate/alias预算/ErrSyntax-only fallback回归，解析fuzz。
6. 最新通知：全量测试、全量race、proddiff必须用 server/scripts/gate.sh（每服务器最多2轮），.64加-p 4；迭代只跑改动包。最终rebase go、干净、不push、不派子agent。

已跑（.64）：
- `rt -- bash -lc 'cd server && go test -count=1 ./internal/api/rules ./internal/server'` PASS 0.433s/0.045s。
- `rt -- bash -lc 'cd server && go test -count=1 ./internal/api/rules ./internal/rules'` PASS 0.925s/5.157s。
- `rt -- bash -lc 'cd server && go test -p 4 -count=1 ./internal/api/rules -v'` PASS 1.533s。

注意：当前 readBody 通过 HTTP JSON facade 读取后转换原生稀疏patch；C27 settings/节点操作与repo旧接口全部集中在adapter；不要把 jsonx 放进核心。新 rulefetch Append 返回(string,error)；yamlx 不允许用 Clone 展开用户图，也不允许把策略/alias错误降级为plaintext。无效旧 native draft 的删除路径先显式拒绝 YAML policy/alias 错误，语法损坏按Python保留不可发布草稿。最终报告需重写为已完成状态。
