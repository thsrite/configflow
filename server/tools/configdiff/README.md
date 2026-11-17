# Config 领域公开 HTTP 差异测试

依据 `server/parity/difftest-config.md`（含 O6）。只启动真正的 Python/Go 公开服务，不导入 Go 业务包，不从 Go 实现推导期望。被测Go是**无build tag的生产二进制**；`build.py`另建一个`configflow_proddiff`公开入口临时进程，只导出实际method/template后立即退出，不接收测试请求。集成0f943623开始该tag会关闭Provider的12秒回退，故不能再用带tag进程验收T06。后台循环、回退时限和HTTP服务均保留生产行为，无源码overlay。

在测试服务器运行，Python 依赖沿用 `~/cf-test/.venv`：

```sh
RT_HOST=sunbroke@192.168.0.32 rt \
  --back server/tools/configdiff/testdata/results.json \
  --back server/parity/difftest-config-findings.md \
  -- bash -lc 'server/tools/configdiff/gate.sh REPLACE_WITH_PUBLIC_GO_COMMIT -p 4'
```

全量统一由上面的包装器先执行官方`server/scripts/gate.sh`，随后在同一个gate锁槽内运行Python configdiff all；官方门禁失败也保留configdiff完整证据，两者退出码分开记录且总退出码不吞失败。包装器只source官方脚本以保留FD 8，无另起race/proddiff。最多两轮共享门禁，.64使用`-p 4`。

迭代用`native_network.sh ~/cf-test/.venv/bin/python server/tools/configdiff/run.py --groups precedence,jwt,auth-environments,redaction,startup,auth-routes --baseline COMMIT`选择相关批次；`--binary /path/to/configflow`可复用本工具生成的无tag生产二进制，必须同时保留其`.routes.tsv`和`.build.json`。工具核对二进制/清单完整SHA256和构建标签；旧带tag二进制或错配sidecar明确拒绝，不以静态routes.tsv猜实际wiring。迭代和最终全量可使用等价的.32或.64，报告注明主机；原WSL .63已停用。

`native_network.sh`在所选测试服务器建立只有lo的网络命名空间和private mount namespace，把owned临时目录绑定到`/etc/nginx`，随后降回调用用户运行测试；不会修改宿主网络、配置或服务。需要服务器安装nginx（Web状态只检查它存在，Step B不会启动真实Nginx）。fixture还验证mount表及临时源目录与挂载目录的inode对应。退出时卸载并删除owned目录。依赖HTTPS fake和Web完整部署要求这个边界，拒绝在WSL/普通宿主网络启动。全量gate包装器会调用此wrapper；RT_HOST也可固定为sunbroke@192.168.0.64，连续复用同一远端副本时应固定主机。网络命名空间内不能下载缺失依赖，首次需要的Go/Python工具链先在测试服务器普通环境安装或缓存。

Web用例必须启用公开Go frontend入口，不能用API-only模式代表完整部署。Python前端HTTP peer为fake，状态/同端口noop/占用拒绝不调用Nginx；成功换端口通过受限`nginx_peer.py`记录精确check/reload并按真实生成配置切换双栈fake HTTP。wrapper仅在private mount namespace把该peer绑定到Nginx可执行文件，不运行真实Nginx，宿主二进制/服务不变。Go前端始终真实运行；比较新端口双栈健康/页面/能力轮换、旧端口关闭，以及失败后旧监听不变和全SQL/保存文件。Nginx专属故障为partial，真实Nginx+Flask叠加留给实验容器/O9/O14，具体合同见behaviour-changes.md的Go取代Nginx条目。

返回码：0 无未批准差异（可以有精确命中已批准决定的严格差异）；1 存在open差异；2 夹具、基础设施或参考前提错误。每次运行先执行比较器篡改自检。Go 未注册的 method/template 标 `go-unimplemented`，保留 Python 参考前提验证，未计通过/失败；未来路由接通后自动比较，绝不从一个业务 404 猜“未实现”。当前注册清单是实际 wiring，`routes.tsv` 的 todo/done 不会把已接通路由永久跳过。

`testdata/results.json` 记录合成用例的编号、具体变体、路由、状态和差异。按批次重跑替换旧结果，其他批次保留。`difftest-config-findings.md` 自动列两侧值，全部来自合成夹具；每次提交的真实覆盖与未完成项在 `parity/progress/difftest-config-b.md`。

夹具通过 **Python backup importer** 建库后复制到两端，不使用生产快照；初始模板也用 Python 默认空配置而非仓库演示资源。默认关闭自动 WebDAV 备份，seed中的启用源必须 interval=0；启动器严格核对，新鲜缓存不绕过检查。需要可刷新源的用例在启动后通过公开订阅/任务接口开启间隔，并观察真实刷新时机。生产 app/main、后台循环、启动缓存清理保留，超过 30 秒的场景须单独列 fake/后台预期。大容量seed选项通过临时JSON文件交给独立Python初始化进程，避免Linux单个命令行参数上限；该选项文件不进入两侧DATA_DIR。

比较包含 status、明确列出的响应头和完整 JSON/YAML/文本/归档结构。Allow、Access-Control-Allow-Methods/Headers、Vary 仅排序其 HTTP token 列表（保留重复/缺项）；Werkzeug HTML 错误页按 O6 比 status 和非 JSON。动态值必须先校验格式、时间窗/格式或 JWT claims，再建单射对应，不能整树 mask。仅逐端点明确 CPython 异常尾文可变化，中文前缀和所有其他字段继续比较。

写入场景设 `writes=True`，读取 SQLite 的 meta/sections/fields（含行位置/revision/JSON 值）和 DATA_DIR 文件，观察失败后的真实结果。SQLite 物理页不比；app.log/锁文件列为专用 X07/T26 的比较边界，不混入普通生成文件。DB 行、生成文件、深层 JSON、YAML、文本、status/header、身份单射和时间窗都有故意出错的自检。新比较类型必须先加同一生产比较器的篡改测试。

当前已有框架、O6 横切多蓝图、全表 A1–A7 与 auth 路由；不宣称已经实现整个 5697 条设计清单。每个后续路由文件应提供具体合法 fixture、独立失败输入、写后 DB/文件观察和编号，不能把全表发一个坏 JSON 当逐行覆盖。

异步任务会先改变内存中的 job/lock，再落盘。启动请求使用 `runtime_writes=True`：Go入口未接通时，后续已接通读取也标记依赖未实现，不拿Python的在途任务与Go空状态比较。源端仍走完整终态/DB/files前提验证；接通后自动进行真实双端比较。

公开服务端口按 fixture 租约保留：同一 fixture 重启时继续保留旧端口，整个 fixture 确认进程停止后释放；新 fixture 可使用内核回收的端口。分配器有超过4096个生命周期的复用自检，并验证活动租约、重启和幂等关闭的隔离。

`--baseline <公开Go源码提交>`标明本轮基线。`finding_reviews`保留原始差异及实际复验：仅compared能closed，附修复提交；仅精确命中`behaviour-changes.md`明确批准条目能accepted，额外响应/存储差异仍open；未跑pending、未实现/依赖blocked。严格case verdict和全差异不被改写，summary另列本轮accepted/open数。`refresh_finding_reviews.py`只根据已记录本轮组重算注释和Markdown，不执行、拼接或伪造case结果。

`helper_cases`是同一次真实R请求对应的源码H标签，不增加HTTP变体，不改变Go尚未实现等状态；只有审计确认经过指定helper guard才关联。被wrapper先拒绝的重复防御分支写入matrix errata，不能借相似错误计为H覆盖。

`artifact-real-metadata-size-boundary`（默认all包含）真实通过公开Surge设置保存>512MiB的合成注释，再验证普通生成成功而预览metadata超限；生产常量没有改。该夹具及request spool放在服务器磁盘的owned临时目录`~/.cache/configdiff-large`，退出清理，避免撑满/tmp tmpfs。实测峰值约8.5GiB，完整all须给足内存；共享服务器上可像下例使用独立scope，具体主机写入报告。降权会清除user bus环境，所以显式传递调用用户的runtime目录。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc \
  'server/tools/configdiff/gate.sh REPLACE_WITH_GO_COMMIT -p 4'
```

大字符串/字节在实际完整比较产生diff之后才压缩报告证据：长度、完整SHA256及首尾字节。状态、路径和差异仍保留，不拿摘要替代比较或把超长值视为相等；对应篡改自检覆盖Unicode、末尾改动和surrogate。
