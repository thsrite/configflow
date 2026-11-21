# O14 切换清单：各模块交接项

本清单供 O14 的切换与发布计划复用，实际执行仍由 O14 / 维护者安排。

- [ ] **A14 / 设计 §7.1：切换后每个 Agent 会一次性显示 `changed`，需要统一全量推送。**
  Go 的 YAML 字节格式与 Python 不同；内容 Fingerprint 继续识别真实文件字节。
  必须逐台确认整包发布、核心加载及部署状态成功，再确认 content sync 为 synced。
  proddiff 的测试副本格式迁移只用于后续完整状态比较，不能在生产中清掉这次 changed。
  证明及原基线回归见 [proddiff README](../tools/proddiff/README.md)。

- [ ] **O9-H2 / L2：配套 Docker Agent 镜像先于或同时于 Go 面板镜像发布。**
  同一 `v*` 标签下 `docker-agent.yml` 推 `config-flow-agent:<版本>`（含预发布，amd64 + arm64），Go 面板发布镜像注入同一标签，
  生成的 Docker run/Compose/HA 命令使用它。切换或公开发布前用 `docker buildx imagetools inspect` 确认该标签存在；
  不存在就先重跑 Agent 构建，不要改用 `latest`（旧入口脚本不识别一次性注册凭据，开启登录后新装 403）。
  见 [o14-switch-plan](o14-switch-plan.md) P13、B.2.1；[差异表 O9-H2 发布补充](behaviour-changes.md)。
