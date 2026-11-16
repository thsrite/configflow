# Config 领域公开 HTTP 差异测试

依据 `server/parity/difftest-config.md`（含 O6）。只启动真正的 Python/Go 公开服务，不导入 Go 业务包，不从 Go 实现推导期望。`build.py` 只读取公开 `cmd/configflow/main.go`，临时 overlay 导出实际 method/template；后台生命周期保留生产行为。

在测试服务器运行，Python 依赖沿用 `~/cf-test/.venv`：

```sh
RT_HOST=sunbroke@192.168.0.63 rt \
  --back server/tools/configdiff/testdata/results.json \
  --back server/parity/difftest-config-findings.md \
  -- bash -lc '~/cf-test/.venv/bin/python server/tools/configdiff/run.py'
```

`--groups precedence,jwt,auth-environments,redaction,startup,auth-routes` 可选择批次；`--binary /path/to/configflow` 可复用本工具生成的测试二进制。最终完整运行使用 .32；命名空间、容器、MITM 或真实网络栈实验只在 .32。

返回码：0 无差异；1 真实比较发现差异；2 夹具、基础设施或参考前提错误。每次运行先执行比较器篡改自检。Go 未注册的 method/template 标 `go-unimplemented`，保留 Python 参考前提验证，未计通过/失败；未来路由接通后自动比较，绝不从一个业务 404 猜“未实现”。当前注册清单是实际 wiring，`routes.tsv` 的 todo/done 不会把已接通路由永久跳过。

`testdata/results.json` 记录合成用例的编号、具体变体、路由、状态和差异。按批次重跑替换旧结果，其他批次保留。`difftest-config-findings.md` 自动列两侧值，全部来自合成夹具；每次提交的真实覆盖与未完成项在 `parity/progress/difftest-config-b.md`。

夹具通过 **Python backup importer** 建库后复制到两端，不使用生产快照；初始模板也用 Python 默认空配置而非仓库演示资源。默认关闭自动 WebDAV 备份，启用源必须 interval=0 或提供新鲜缓存；启动器核对这一约定。生产 app/main、后台循环、启动缓存清理保留，超过 30 秒的场景须单独列 fake/后台预期。

比较包含 status、明确列出的响应头和完整 JSON/YAML/文本/归档结构。Allow、Access-Control-Allow-Methods/Headers、Vary 仅排序其 HTTP token 列表（保留重复/缺项）；Werkzeug HTML 错误页按 O6 比 status 和非 JSON。动态值必须先校验格式、时间窗/格式或 JWT claims，再建单射对应，不能整树 mask。仅逐端点明确 CPython 异常尾文可变化，中文前缀和所有其他字段继续比较。

写入场景设 `writes=True`，读取 SQLite 的 meta/sections/fields（含行位置/revision/JSON 值）和 DATA_DIR 文件，观察失败后的真实结果。SQLite 物理页不比；app.log/锁文件列为专用 X07/T26 的比较边界，不混入普通生成文件。DB 行、生成文件、深层 JSON、YAML、文本、status/header、身份单射和时间窗都有故意出错的自检。新比较类型必须先加同一生产比较器的篡改测试。

当前已有框架、O6 横切多蓝图、全表 A1–A7 与 auth 路由；不宣称已经实现整个 5697 条设计清单。每个后续路由文件应提供具体合法 fixture、独立失败输入、写后 DB/文件观察和编号，不能把全表发一个坏 JSON 当逐行覆盖。
