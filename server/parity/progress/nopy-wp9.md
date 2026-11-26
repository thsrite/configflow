# WP9 — proddiff「Go 基线 vs Go 候选」（2026-11-22）

设计：`server/design/no-python-tests.md` §2.4 和工作包表 WP9。分支 `go-nopy-wp9`（基于 `go@b85aa02f0`）。
用法和规则见 `server/tools/proddiff/README.md`「两种参考模式」。

## 做了什么

- `server/scripts/proddiff.sh`：`PRODDIFF_MODE=python`（默认）/`go`。go 模式要求 `PRODDIFF_BASE_TREE`，
  从基线树构建 `configflow`（`configflow_proddiff` tag）和 `tools/proddiff/generator`，分别代替 Flask
  和 Python 生成器；比较器在独立工具 venv（`~/cf-tools/.venv`，由 `server/tools/requirements-tools.txt`
  建立，文件哈希变化时在 flock 下重建）运行，命名空间内不把仓库根放进 `PYTHONPATH`。
- `run.py` / `generators.py`：左侧进程、路由清单（`base-routes.tsv` + `.ready/.loops/.startups`）、
  生成器命令、标签（`base`）按模式切换；go 模式只比较两边都注册的路由（候选新增计 `candidate_only_routes`，
  候选删掉的路由按覆盖回退失败）；A14 收据格式迁移只在 python 模式做；Flask→Go 的已批准差量
  （O9 安全头/安装 token、L1 回滚、C07、Agent 版本配对、离线传输文本、A18、M18）只在 python 模式生效；
  公开键名单在 go 模式从 Go 源码提取，不读 `backend/`。
- Go-vs-Go 固有差异（同一份代码两个独立进程）：
  - 安装脚本 / docker compose / docker run 里每个进程自己生成的 O9-H2 注册 token：精确正则遮蔽（只遮
    `_REGISTRATION_TOKEN=` 和 `"registration_token": "` 后的 43 位 token）。
  - 运行中新检测到的通知事件的 `first_seen_at`/`version`：`proddiff-normalize.json` 新增两条
    `"mode": "go"` 的 tolerance 600 规则（规则新增可选字段 `mode`，python 模式不加载这两条）。
  - 六条磁盘生成路由的 `header.ETag`：按 PD-1 的做法登记进 `parity/proddiff-baseline-go.txt`。
- `gate.sh`：加 `PRODDIFF_MODE` 开关（默认 python，WP11 再切）；未知模式或 go 模式缺基线树在开工前退出 2。
  `test-gate-routing.py` 加对应控制测试。
- 删除只为 Python 存在的选项：`--a13-proof`（`a13_proof.py`、`api/agents.TestRealPreparationProof`）、
  `--a18-proof`（`a18_proof.py`）。两者都启动 Flask 或真实 Python 准备函数，延期条目早已清零。
- `test_proddiff.py`：新增 `GoModeTests`（Python 差量不生效、`parse-base`、Go 源码键、基线生成器、
  main 以两个 Go 进程运行且注入差异报 new、token 遮蔽、按模式加载规则）；Flask/backend 相关的 3 个用例在
  工具 venv 中跳过。

## 结果（.64，快照 20261116）

| 运行 | new | stale | 唯一差异 | self-check | 路由/用例 |
|---|---|---|---|---|---|
| python 模式（默认） | 0 | 0 | 19（均已登记） | 23 项 PASS | 205 路由 / 2831 用例 |
| go 模式（基线 `go@b85aa02f0`，候选本分支） | 0 | 0 | 6（ETag，已登记） | 23 项 PASS | 205 路由 / 2831 用例 |
| go 模式首次（未加 token/通知归一前） | 14 | 0 | 14 | PASS | — |

负向对照（候选树临时改动，跑完即撤销，未提交）：

| 注入 | 结果 |
|---|---|
| 响应字段：`GET /api/agents/latest-version` 的 `version` 加后缀 | exit 1，`new=1`：`GET /api/agents/latest-version	body.version	value` |
| 存储写入：通知新事件写入时 `occurrences` 初值 1→2 | exit 1，`new=1`：`GET /api/notifications	body.items[1].occurrences	value` |

## 留给 WP11

- 默认模式切到 go，并决定 `PRODDIFF_BASE_TREE` 的来源（上一个发布 tag 或上一次通过门禁的 `go`，
  gate 需要先导出该树）。
- backend 删除后：python 模式、`flask_worker.py`、`python_generators.py`、`content_policy.wait_for_content`、
  `provider_policy.py`、`baseline/` 收据迁移工具、`proddiff-baseline.txt` 一起删除。
