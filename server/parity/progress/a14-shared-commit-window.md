# A14 → O3-6c：最终共享提交窗口

协调者决定（2026-11-18）：由 **O3-6c** 落地共享修复；A14 只保留回归和自身调用点。
原型已导出为 [git format-patch](a14-shared-commit-window.patch)，没有把原型应用到 A14 的运行源码。
该文档提交只包含交接记录及 patch；O3-6c 可用 `git am` 或三方应用，以它为起点调整共享 API。

**高严重度复现：真实 Runtime 在 Store.BeforeCommit 期间关机，SQLite 仍会 COMMIT。**
`lifecycle.Detach` 和事务进入前检查均不足以覆盖序列化/回调之后的窗口。

A14 回归：`internal/deploy/runtime_boundary_test.go::TestShutdownAtSQLiteCommitKeepsWholeDocument`。
测试使用 Configure/Start 配置的全局真实 Runtime，BindRequest 后调用 BeginDeployment；
在 Store.BeforeCommit 回调里同步 Stop（即真实 Runtime.Shutdown），然后放行 COMMIT。
预期为 context.Canceled、完整 DB/文件不变，并且取消的 preparation key 可以重新取得。

复现命令（测试服务器 .64，经 rt）：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -v -run "^TestShutdownAtSQLiteCommitKeepsWholeDocument$" ./internal/deploy'
```

未加共享修复时：BeforeCommit shutdown 返回 nil，完整 document 已改变。协调者最新决定（2026-11-18）：A14 不等 O3-6c，回归暂用显式 `t.Skip("等 O3-6c…")` 保留完整复现体；O3-6c 合入时必须删除 skip 并通过该回归。此 skip 不表示问题已修复。
验证过的原型让这个回归通过（0.28s）：

- 新增 Store.CommitContext，检查进入和 SQLite COMMIT 前的 ctx.Err()，后者在 BeforeCommit 回调之后。
- repo.commitStore 传递 ctx，包含 encodeRows 等序列化之后的最终检查。
- repo 文件提交提供内部 context 版本，主文件和 .bak 的 rename 前检查；旧入口保留兼容 wrapper。
- 原型仅 5 个现有文件，无新的依赖；patch 内有 private commitStore 测试调用的签名调整。

另一个测试现象已确认属于夹具：独立 Runtime.NewRuntime 与全局 Spawn 混用。
生产只有一个全局 lifecycle Runtime；A14 测试已改为 Configure/Start/BindRequest/Stop，
未修改 Spawn 的生产调度合同。
