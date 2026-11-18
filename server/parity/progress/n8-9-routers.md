# N8-9 routers / routersplit S pass

状态：实施中；尚未交回、未通过最终定向验证或 Opus HR 复核。分支 go-n8-9-routers，起点 go@d4fab6441。只在 .32 跑测试，不 push、不跑完整门禁。

## 范围与接线

- routersplit：删除 pyhelpers.go，typed Profile/Marker/Context/Session/SplitState 与 yaml.Node 为业务边界。保留唯一 legacy.go 给尚未迁移的 Agent/setup/gateway 调用者；PythonClass 只在测试中为冻结语料分类，不在产品代码模拟异常。
- routers：存储字段走 model.Router / model.String / model.Int；窄运行时投影保留原始字段，设置编辑用共享 SetJSON，避免 R5 默认写回覆盖存储。管理事务接 repo.UpdateProfile，设置 CAS 接唯一 SaveRouterSettingsJSON；旧 SaveRouterSettings 只做 codec。
- 通知接 native notifications.Inputs/SaveRouter，task slot 接 SetRowBuilder；定时扫描只复制路由器子树。发布/管理/pending 的配置读取接 repo.Config。
- 暂留 legacy_rulefetch.go 为 rules 分支未合入时的唯一 adapter，操作开始只转码消费的 rule_library/proxy_groups/system_config，逐条规则复用同一环境。待 batch-8 后接正式 SourceConfig。management_repo.go:saveWorkspacePolicies 按协调者要求完全不改，等待 DNS owner 合入。

## 常设问题类与防线

1. 不替换真实驱动的 timeout 参数；现有 loopback fakes 保留。
2. 原始设备 body/header、GET/DELETE 无 body、批脚本、回读/补偿由 R04–R17 既有独立 Python/HTTP 测试守住；请求编码未修改。
3. 设备日志与错误凭据继续统一 internal/redact，不写第二套 scrub；新窄 model 不把用户名当密码处理。配置导出的用户名隐藏仍按既有导出合同。
4. RouterJobLock 仍是唯一 L1；先 try-lock 再身份验证，busy 优先级不变；拿锁后检查 shutdown。管理/存储提交沿原事务叶锁及 ctx.Err 路径，真实 Runtime 回归待最终执行。
5. RouterOS/iKuai 外部回复的 Row.text/flag/number 保留并注明厂商字符串/数值兼容；用户存储连接/凭据不能用数字或容器伪造成合法身份。
6. 增加路由器读取与规则环境的分配基准/字节预算，额外 1 MiB 无关 YAML 不应在每项循环重新解码。

## 语料与批准变化

原 r01/r02/r03 .jsonl.gz 不改。n89_reference.py 在真实 Python 业务前只注入明确的 R5/原生请求策略，产生绑定原始行 SHA256 的独立完整期望；普通行仍走旧比较。空身份不证明拓扑；损坏 Agent 绑定在读会话前 fail-closed。原 N7 YAML 分类 oracle 保留。

正在核对逐行差异，不允许用 Go 实际失败输出录制新期待。R01 的非文本身份、非 bool TLS、name 字段及共享整数接受变化另列 sidecar。R02 的错型 record/list/bool、绑定身份与取消 Python 内部异常另列 sidecar。尚未宣布这些核对完成。

## 已执行与待执行

- 初次编译/局部回归已通过 routersplit 两条基本测试、routers task rows、repo 保存设置；初次 repo 非字符串 ID 错误优先级退化已恢复到先检查 profile 的原路径。
- 首次 R02 replay 揭露 marker `"1"` 被 json.Number 接受，已用原始 version token 严格校验修复；标记字段原对象顺序在 legacy codec 保留。
- 正在运行（产品树早于最后的读取/守卫接线）：
  `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && go test -race -p 4 -count=1 -timeout=12m ./internal/routers ./internal/api/routers ./internal/repo -skip "Corpus|Reference|Golden|RecordedPython"'`
- 下一步：收口独立期待与 pyref metadata；定向 race/所有 R04–R17 HTTP 边界；跨包 Corpus|Reference|Golden；新 fuzz60s；分配量；gofmt 与 go vet；登记 behaviour-changes；fresh Opus HR 复核；rebase go；提交并交回。

## 中间验证结论（2026-11-19）

- 首次 race：routers 384.292s 与 api/routers38.200s 失败，repo340.200s通过。失败不是隐藏豁免：R12 source=nil 的缺失/显式默认 CAS、X1 数值规则ID和坏 guard、R15 probe 无关字段保真。原断言保留，分别修复后定向原用例已通过（R15 api1.886s）；显式设置等于运行时默认仍需写入，已新增 explicit 成员意图，Extra 删除走共享 Clear/Fields.Delete。
- R01 独立期望729行，原完整矩阵正常条目其余不动；R01和新增N89/修复定向的 routers12.195s/11.281s已通过（最后 explicit+默认区别后再做最终验证）。
- 拓扑身份进一步收紧：Context 保存原始ID错型证据；ValidateTopology 在 native入口明确拒绝数值/容器身份，optional null link仍是无关联。原始 marker.version 仍只接受整数token1，不接受字符串或1.0。
- R02 在严格身份政策下重新由真实Python生成2948行独立期望；`TestCorpus|TestEdgeCorpus|TestRecordedPythonTests` 全部通过3.353s，原8323行总输入不减，原gz未改。中间期望生成器曾发生kwargs签名错误，已修正后独立重生；没有用Go答案覆盖期望。
- 已启动 fresh Opus5.5/high HR复核（agent abb5e93c-ea20-4fe2-b6a8-7836041589ff）；只读当前产品，报告写review-n8-9-routers.md。最终race/跨包语料/fuzz/vet仍待完成。

## HR修复与依赖基点

- Opus首轮报告review-n8-9-routers.md发现F1高（锁内校验阻止坏记录修复/删除）、F2中（校验了无关拓扑），以及F3坏选择列表空写回、F4未知字段修改错误、F9既存坏guard被当作撤回等。均已按问题类补修；永久HTTP回归在.32通过（api4.109s），坏存储PUT修复/DELETE200，probe/routes400；坏列表无关改名400且原完整行/revision不变。未知字段添加/修改/状态删除及原metadata不变、坏active guard阻止设备访问的回归也通过。
- Opus首轮自行在.64做负控，违反本任务仅.32约束；其.64结果不算本组验收。作者已在.32重做对应关键回归，后续复核明确限制.32。
- F2收窄后独立R02期望2553行，原8323总输入不减，真实Python重生；R01请求interval恢复strict JSON integer后独立期望725行。最后这轮`TestCorpus|TestEdgeCorpus|TestRecordedPythonTests|TestR01Corpus|TestN89|TestProfileSessionsHook`：split4.385s，routers11.084s，api4.109s全过。
- 首次完整跨包语料运行被排队rt的同步改写远端oracle/sidecar打断（ValueError→RouterSplitError及源码metadata漂移），不能计PASS。停止并发同步同项目；后续每条rt完成后才发下一条。
- 最后完整routers/api race在eceb4e2e6树通过375.507s/28.791s；split只有旧completed=1断言失败，后来的真实Python独立session fixture已通过5.232s；本轮HR修改后会再跑相关定向race。
- 协调者明确允许rebase到rules最终复核分支go-review-n8-9-rules@9989da440以接正式SourceConfig/CacheReadPath/Download。batch-8合入go后再rebase到go，重复patch会跳过；不复制owner文件/业务。生成只读入口等同PeekCacheWithFormat，不迁移cache；saveWorkspacePolicies继续不动。

## 正式 rules 接线与最终复核

- 已完成rebase到go-review-n8-9-rules@9989da440；本组重写后的两个提交为c8b4ab7cc、117cf39c0。只有behaviour-changes尾部冲突，保留双方登记。
- 删除legacy_rulefetch.go和syncTransportKey；native SourceConfig/ValidateRemote/CacheReadPath/WriteCache/DownloadRequest接线完成。环境在sync已核对控制摘要和manifest之后建立，同一不可变config贯穿读取、逐项下载和最终写cache；设置摘要只算一次，每条下载只拼摘要+URL。ctx不得跨snapshot复用已写入注释。
- 只读缓存使用typed CacheReadPath上的有界peek，不调用会迁移旧摘要的ReadCache；PeekCacheWithFormat尚未在依赖基点，叠batch-8时以owner正式只读入口为准。新增真实legacy缓存读取回归：内容完整相等、旧mtime和DB revision不变、native文件仍不存在，race通过1.587s。saveWorkspacePolicies自d4fab6441逐字未动，脚本核对为True。
- 全管线分配：真实loadSyncSources+原生loopback下载和transport；cache 1/16规则额外1MiB shared/config的B/op为57365/56277、898508/882945；16规则下载多256KiB settings，native增3700694B，隐藏环境负控增63378357B，同一6MiB预算正控通过且负控明确超限。实际生产writeSyncSources循环也加预算：race16规则2118706/5813900B，增3695194B（<6MiB）。没有用只测helper的基准冒充完整管线。
- 新peek测试首次把文档网段的IPCount误期望为1，属于新增测试错误；改为断言实际读取的原始内容和无副作用，不改变原冻结分类期待。最初缺legacy子目录的fixture也已补齐。
- Opus追加复核F1–F9全部关闭，同意verified，条件为作者收尾检查通过；只读复核未重复任何测试。作者随后仅补实际写缓存预算、环境scope注释和readonly回归。余下：最终定向race/HTTP、跨包语料、fuzz60s、vet/gofmt、最新go重base状态与最终报告。
- 数量澄清：8323是随机corpus6000+edge2323；此外recorded2595，routersplit三份共有10918条自身调用（原gz合计13641，含ippresence的2723行）。不减分母。R01固定10000行，725行独立native策略期望。
