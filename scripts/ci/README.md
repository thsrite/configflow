# 回归检查与测试分片

这里面向项目维护者。普通安装无需运行测试，部署步骤见 [README](../../doc/guide/install.md)。命令与当前 [checks.yml](../../.github/workflows/checks.yml) 和 [分片校验工具](shard_tests.mjs) 对齐。

## 工作流何时运行

`checks.yml` 当前提供 `workflow_dispatch`（手动运行）和 `workflow_call`（供其他工作流调用），没有声明 `pull_request` 触发。不能仅因打开 PR 就认为整套回归已自动运行，应查看实际 Actions 记录。

同一工作流和 ref 的回归不会取消已运行任务，`cancel-in-progress: false`。

## 检查范围

| Job | 执行内容 |
| --- | --- |
| `backend` | Go server（`server/`）：`gofmt`、`go vet`、build tags 与 `go test -race ./...` |
| `frontend-tests` | 2 个 Vitest 分片，保存实际测试报告 |
| `frontend-build` | 收集完整测试文件清单，执行 TypeScript 检查与生产构建 |
| `frontend` | 等待分片和构建成功，运行分片工具自测并验证实际报告覆盖完整文件清单 |
| `agent` | 按 Agent 的 `go.mod` 安装 Go，运行 `go test -race ./...` |
| `node-probe` | 按测速组件的 `go.mod` 安装 Go，运行竞态检查和构建 |

## 前端如何验证完整覆盖

在 `frontend/` 中先收集完整文件清单，再执行两片：

```bash
mkdir -p ../.ci-results
npx vitest list --filesOnly --json > ../.ci-results/frontend-all.json
npm test -- --shard=1/2 --reporter=default --reporter=json --outputFile=../.ci-results/frontend-1.json
npm test -- --shard=2/2 --reporter=default --reporter=json --outputFile=../.ci-results/frontend-2.json
```

返回仓库根目录后验证：

```bash
node scripts/ci/shard_tests.mjs validate \
  --expected .ci-results/frontend-all.json --expected-key file \
  --shard-key testResults.name --path-prefix frontend/tests \
  .ci-results/frontend-1.json .ci-results/frontend-2.json
```

## 分片工具自身的回归

```bash
node --test scripts/ci/test_shard_tests.mjs
```
