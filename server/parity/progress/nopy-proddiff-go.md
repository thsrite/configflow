# proddiff 比较器改写成 Go（2026-11-23）

分支 `go-proddiff-go`（基于 `go@3c64f4bb6`，已含 WP9 的 Go-vs-Go 模式）。设计：`server/design/no-python-tests.md`
方案 C（切换时删除 Python 模式），WP9 记录见 `nopy-wp9.md`。用法见 `server/tools/proddiff/README.md`「两种参考模式」。

## 做了什么

- `server/tools/proddiff/comparator`（Go，`package main`）：移植 `run.py` 的 Go-vs-Go 模式全部比较器，以及
  `generators.py`（清单、两侧生成器、成功输出下限）、`artifact_policy.py`（手动 artifact 证明与配对）。
  Python 模式（Flask、Python 生成器、A14 收据迁移、只为 Flask→Go 的已批准差量）不移植，切换时随 run.py 删除。
  - 语义保持：JSON 深比较（int/float/bool 区分、重复键失败、NaN/Infinity、大整数）、YAML/ZIP/文本解析、
    内嵌 content/config/compose、Agent 配置摘要校验、Agent 缓存身份配对、O9-H2 注册 token 遮蔽、
    `proddiff-normalize.json` 全部模式（含 `mode`、look-around 正则用现有依赖 regexp2）、单侧传输重试和
    bracket 重问、分阶段并发、用例枚举（与 Python 的 urlencode/quote 一致）、基线文件格式、覆盖回退、
    生成器下限、23 项自检、`summary.json`/`coverage.json`/`differences.tsv`/`report.md`、控制台输出和退出码。
  - 已知实现差别（README 有说明）：YAML 用服务端 yamlx/yaml.v3（不做 PyYAML 1.1 隐式类型，非核心 tag 算解析失败）；
    同一小写头名多种写法时确定性选择；`PRODDIFF_CORPUS=1` 时额外写私有 `corpus.jsonl`。
- `proddiff.sh` / `gate.sh`：新开关 `PRODDIFF_COMPARATOR=python|go`（默认 python，切换前再改）。go 只能配
  `PRODDIFF_MODE=go`，不建工具 venv、不需要任何 Python；非法组合在开工前退出 2（`test-gate-routing.py` 加控制测试）。
- 测试：`test_proddiff.py` / `test_artifact_policy.py` 中与 Go 模式相关的用例移植为 Go 测试（70 个顶层测试），
  只属于 Python 模式的（O9 安全头、C07、L1 回滚、A18、M18、Agent 版本配对、离线传输文本、收据迁移、Flask
  观察器）不移植，改由 `TestGoModeAppliesNoPythonReferenceDeltas` 断言它们在 Go 模式下不生效。
- `crosscheck.py`（临时）：同一语料喂给 run.py 与 Go 比较器，逐条比较；切换时与 run.py 一起删除。

## 对照结果（.64，快照 20261116，基线树 `go@3c64f4bb6`，候选本分支）

两个实现同跑（同一对基线/候选，各自完整运行一次）：

| 比较器 | exit | new | stale | 唯一差异 | 自检 | 用例 |
|---|---|---|---|---|---|---|
| Go（`PRODDIFF_COMPARATOR=go`） | 0 | 0 | 0 | 6（PD-1 ETag，已登记） | 23 项 PASS | 2831 |
| Python（run.py，Go 模式） | 0 | 0 | 0 | 6 | 23 项 PASS | 2831 |

去掉 `loadavg`、`transport_retries`、`counts.bracket-rescued` 后 `summary.json` 完全相同（含键顺序）；
`differences.tsv`、`coverage.json`、`generator-jobs.json` 字节相同。

同输入逐条对照（`crosscheck`）：Go 运行写出的 2846 对真实回复（HTTP 2831 + 生成器输出）加上
`crosscheck.py mutate` 的带种子变异（左右交换、JSON/YAML 结构编辑并重新编码、文本改行/插行/CRLF、
响应头改值/删除/新增、状态码和类型变化），共 11 695 条；其中 4 972 条有差异。
run.py 与 Go 比较器的差异列表 **逐条一致，0 条不一致**，两边都没有异常。

负向对照（候选树副本里临时改动，跑完即删，未提交）：`GET /api/agents/latest-version` 的 `version` 加后缀，
通知新事件的 `occurrences` 初值 1→2。两个比较器输出相同：

```
baseline: new=2 stale=0
NEW	GET /api/agents/latest-version	body.version	value
NEW	GET /api/notifications	body.items[1].occurrences	value
EXIT=1
```

## 切换时要做

- `gate.sh` / `proddiff.sh` 默认改成 `PRODDIFF_MODE=go PRODDIFF_COMPARATOR=go`，去掉两个开关的 python 分支。
- 删除 `run.py`、`generators.py`、`artifact_policy.py`、`content_policy.py`、`provider_policy.py`、
  `flask_worker.py`、`python_generators.py`、`test_proddiff.py`、`test_artifact_policy.py`、`crosscheck.py`、
  `baseline/`、`server/tools/requirements-tools.txt`、`parity/proddiff-baseline.txt`，以及 `security_headers.py`
  （只被 run.py 的 Python 模式使用）。
