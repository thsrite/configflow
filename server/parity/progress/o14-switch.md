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
