# O14 无 Python 切换预演

预演以 `go@7feb6e125` 为基线。裁定 C 的删除、Go 门禁、CI 和 golden replay 合在同一提交，测试依赖按候选树的服务边界显式组装。

- `backend/` 仅保留 Go Agent 与 Node Probe。仓库跟踪的 `*.py`、`*/difftest` 包及 `server/tools/pyref/` 已清零，保留已提交语料。历史实验从切换前候选树运行。
- `PRODDIFF_MODE` 和 `PRODDIFF_COMPARATOR` 均默认 `go`。`skipaudit/allowed.txt` 含 0 条有效记录。
- 回放语料中更新了 Agent 指标历史与规则库列表的各一项预期。前者现在读取与心跳共用的指标服务，后者读取实际语料并识别出仅包含域名。
- 旁路入口的截图工具使用 Go 面板和合成数据。其余依赖已退役面板或参考脚本的旧浏览器入口可在候选树查阅。

完整无 Python 门禁由协调者执行。

指定测试服务器已通过 Go 服务端格式、vet、`configflow_proddiff` 构建、门禁路由与受影响包竞态回归（99 个包通过，4 个包无测试文件）。Go Agent 全量竞态回归、前端类型检查和 3,469 项 Vitest 用例均通过。旁路入口截图工具已通过浏览器验证。
