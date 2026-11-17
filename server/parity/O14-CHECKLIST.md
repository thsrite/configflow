# O14 切换清单：各模块交接项

本清单供 O14 的切换与发布计划复用，实际执行仍由 O14 / 维护者安排。

- [ ] **A14 / 设计 §7.1：切换后每个 Agent 会一次性显示 `changed`，需要统一全量推送。**
  Go 的 YAML 字节格式与 Python 不同；内容 Fingerprint 继续识别真实文件字节。
  必须逐台确认整包发布、核心加载及部署状态成功，再确认 content sync 为 synced。
  proddiff 的测试副本格式迁移只用于后续完整状态比较，不能在生产中清掉这次 changed。
  证明及原基线回归见 [proddiff README](../tools/proddiff/README.md)。
