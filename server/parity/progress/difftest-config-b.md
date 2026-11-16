# Config differential Step B 进度

分支 `go-difftest-config-b`；依据 O6 审过的 `difftest-config.md`。只编辑 tools/difftest/testdata、进度与发现清单；未读 Go 业务实现，未派子 agent。**任务进行中，未交最终验收。**

## 批次 1：框架、O6 横切起步、auth.py

- 真进程：Python `-m backend.app` 和 Go `cmd/configflow`；测试 overlay 只在公开入口导出实际路由，未禁用后台循环/未替换业务逻辑。
- Python 默认空配置→备份 importer→独立副本，固定 TZ/JWT/工作目录/日志路径约定；auto_backup 关闭、无定时刷新源。X08 启动孤儿缓存清理及 DB/文件核对通过。
- 已编码 X02 每非全局代表的 path/query/header profile-before-auth、全局蓝图对照；三认证环境的默认/显式 Mihomo D1–D8；X03 外部内部调用头；T29 双向签发、无 exp、字符串 exp/iat、未来 nbf/iat、HS512/RS256/none、错 key、随机 key 重启失效。
- X01 当前实现 profile 描述的 raw/percent/double-percent，以及导出的脱敏/原始 send_file 两条形态。原始备份 direct_passthrough 保留全文，不能错断言为全局脱敏。其余蓝图、深度、native/editor/content 豁免尚待补齐。
- `backend/routes/auth.py` R011–R013 的所有 S/F/B 编号已编码（含中文账号、8192密码、错误 method、三类异常内容类型、A1–A7 和 B90 JWT）。未用宽泛错误 mask：只忽略明确 CPython 异常尾文，中文前缀仍检。
- DB/文件比较器已接入 `writes=True`；本批 auth 无配置写。后续每个写场景须实际启用并校验相应行/文件。

最近迭代（.63，2026-11-17）：

```text
~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups precedence,jwt,auth-environments,redaction,startup,auth-routes
self-check: 9 tests OK
compared=73, go-unimplemented=125, different=0, design_errors=[]
request_variants=198, named_matrix_cases=45, seconds=17.37
categories（请求变体）：S=11 F=58 B=6 T29=11 D8=12 X01=9 X02=81 X03=9 X08=1
```

125 个“Go 尚未实现”是已编码且 Python 前提验证过的具体变体，非失败也非通过；具体 method/template 与编号见 `tools/configdiff/testdata/results.json`。45 是去重 R 编号，包含在三认证环境重复运行的拉取场景；不得与 198 个变体或 Step A 5697 设计数混用。当前未发现实际业务差异；Allow 无序列表是明确 HTTP 语义比较，缺方法的篡改自检必报错。

## 继续顺序

1. 补全 X01 各蓝图/敏感键/深度/native/editor/生成 content 豁免，以及 X08 旧 node_test_dns 启动迁移边界；T27 实际 tick 单列 .32/隔离 fake。
2. 按路由文件补设置、profiles、共享资源、配置生成/OxiDNS/MosDNS、rules/groups、异步和其他文件；每例对应矩阵编号，todo 路由也编具体 fixture。
3. H helper guards、T/G 子变体须逐项参数化/审可达性；不可 HTTP 达的 OS/内部 guard 按 O6 标归属，不拿双方 500 算成功。
4. 每批写后 DB/文件实测、比较器 mutation；差异列 findings，由协调人派移植 agent。
5. 全量 .32 运行、rebase go、干净工作区、不推送后才最终交回。

## 批次 2：A1–A7 全展开，X01 多蓝图与 native 豁免，X08 迁移

- 从矩阵明确的 A1–A7 范围生成 **206 路由 × 7 = 1442** 个实际鉴权请求（有效 config_token 已配置，证明其不能越权管理 API）。这类输入有意使用缺失业务对象 ID，因为应在对象查找之前拒绝；不算该对象/校验业务分支覆盖。
- X01 增加节点、订阅、规则、组四种文本的原文/单次编码/多次编码/plus 编码；任意层敏感键删除、126–129 深度边界与保留非秘密 sibling；aggregation/library/ruleset/custom/MosDNS/domain/tasks/settings/notifications/auth/logs 代表。OxiDNS 原生 API 凭据通过所有 editor/render/common/native/template/save 默认与显式 profile 入口测试；proxy editor 的例外值也经真实写入及 DB/文件比较。
- 原始 config export 是 send_file/direct_passthrough，确实保留可还原全文；脱敏 export 和普通 JSON 则须清洗。两者明确分开断言。
- X08 旧测速 DNS：合成旧 endpoint + 保留的 `https://dns.example/dns-query`，Python 清掉旧 endpoint/Agent 元数据并记录通知；Go 保留旧数据。按 O6 的退休结论标 **O12-separate**，完整 DB/文件观察共 13 处差异均落入显式允许的迁移路径；任何其他路径仍报真正 difference。详见 findings。
- 实际路由未注册且 routes.tsv=todo 才标“Go 尚未实现”，不再向 Go 发无意义请求；declared done 却未注册会报 wiring 差异。注册后原用例自动比较。
- 新增 ZIP/tar/binary 篡改自检，合计 **10 项自检**；case ID 与其矩阵 route 也逐项校验。

.63 完整现有批次运行（54.84 秒）及 X08 完整观察补跑（1.42 秒）：

```text
~/cf-test/.venv/bin/python server/tools/configdiff/run.py
compared=632 go-unimplemented=1058 O12-separate=1 different=0 design_errors=[]
request_variants=1691 named_matrix_cases=1480
去重矩阵 R 类别：S=10 F=1465 B=5
请求变体类别：S=11 F=1500 B=6 X01=59 X02=81 X03=9 X08=2 T29=11 D8=12
self-check: 10 tests OK
```

仍未完成：X01 artifact/content 与 profile export 补充、极深解码/循环容器的 HTTP 可达性说明；统计类固定数字响应没有用户文本回显，不能硬造 X01 成功。X08/T27 实际 tick/退避/单飞尚待 fake 单列。所有 H/G/T 和其他路由文件的合法业务场景仍待推进；以上数字不代表 1480 个独立业务功能或整个矩阵通过。
