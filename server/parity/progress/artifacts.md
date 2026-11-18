# 手动冻结 artifact 接线（HR，ported，待独立 Opus 复核）

分支 `go-port-artifacts`，起点 `go@03257978f`。本报告只描述三个手动导出入口；原 C34
其余入口的既有复核结论保持。所有验证固定在指定 .32 测试服务器，使用合成夹具；
proddiff 仅在既有断网命名空间内读取离线快照副本。没有设备发布、生产连接、push 或子 agent。

## 实现与 owner

- 两个 POST 使用 typed `configgen.Candidate`，先固定 profile ID、配置 revision 和输入快照，
  再复用原 Build 的生成实现。`Candidate` 只含字符串、字节和 `json.RawMessage`；新入口
  不引入 jsonx/pystr，不复制 converter、归档生成或发布算法。现有 converter 的旧类型适配
  仍留在原 configgen 文件内，未扩散到新 API/发布 owner。
- `deploy.GenerateArtifact` 使用 A14 的 `RunDetached`、`Pack` 和 Manager-owned `Previews`。
  只生成冻结候选，不创建 deployment reservation，不上传、不激活设备。文本取本次 render
  返回的字节，不读取会被并发请求覆盖的 generated 路径；DNS ZIP 的全部成员按原顺序冻结。
  对 ZIP 成员检查安全相对路径、重复、文件数、单文件/总大小，再由 Pack 校验完整 manifest。
- GET 使用同一个 `LoadPreview(...,"manual")`、`Preview.Zip/File`。保留认证、一小时 TTL、
  归属检查、文件白名单、MIME、附件名、no-store；不存在的文件404，失效预览409及
  `code=preview_changed`，未知 action404。HEAD 只读取，正文为空。
- 增补共享 reader 的根目录 symlink 拒绝：只拒绝最后一级 artifact 目录/文件不足以防止
  `.generated-previews` 自身指向另一仓库。原文件/目录防护、归档摘要校验不变。
- 三个 routes.tsv 从 wip 改 done；C34 新范围标 ported。精确删除34条 deferred→A14/A21
  artifact baseline，不增加替代豁免。

## HR 问题类别逐项说明

| 类别 | 如何避免、对应证据 |
| --- | --- |
| 共享 client 行为、真实参数、raw body/headers | 没有新增设备/HTTP client。复用现有生成 owner；HTTP 测试逐字节比较预览、file 和 ZIP 内字节，断言 MIME、附件名、Cache-Control；原 A14 HTTP 调用方定向回归保留。Runtime 中途关机使用生产宽限期，不缩短 seam。 |
| 错误分类和中文提示 | 复用 C34 `failure`、RequestBaseURL 和 A14 PreviewChanged。未知服务400、失效/外来预览409、缺文件/未知 action404。新增中文常量仅缺文件消息；Flask 错误类、状态、中文消息有对照。 |
| 非空身份、跨 Agent/仓库泄露 | 固定 manual owner，Load 拒绝其他 Agent；发布 manifest 和私有记录相互校验。GenerateArtifact 显式绑定 Manager 的 repository；外来 ctx 的仓库不能作为生成源。保存的 Source 不含 agents。外来仓库哨兵、foreign record、另一 Manager 读同 ID 均有拒绝断言。 |
| id 重用、回滚归属、fresh proof | 不执行设备 undo/delete/commit；artifact ID 为随机能力标识，加载时校验记录 ID、归属、TTL、manifest 和归档摘要。读取只交付冻结字节，不按当前配置重生成。 |
| 重复扫描和整表 decode | 捕获一次 profile/revision，复用本次生成结果；成员按归档列表一次遍历，map 去重/检索，无逐成员重新解析整份配置。 |
| 请求断连、关机写窗口 | owner 内 RunDetached/Capture Detach；进入 owner、生成文件提交和 Preview.Save 提交沿用 ctx 检查。真实 Runtime Start/BindRequest/Shutdown：五引擎断连完成、关机后不写；已完成 render、正在等待真实 preview mutex 时关机，不建立新预览。已开始的 Preview.Save 原有原子批次语义不变。 |
| 锁顺序、后台任务和释放 | 不新增锁或裸后台工作；RunDetached 复用 lifecycle 跟踪，Previews 复用同一 Manager 的叶锁及原配额/驱逐逻辑。测试 goroutine 仅用于可观察的锁等待窗口。 |
| 路径与持久数据 | Preview.File 只接受 manifest 中的规范相对路径；无磁盘路径拼接读取用户 path。越界、缺失、symlink 根/目录/record/archive、损坏归档有测试。冻结结果在草稿更新、默认 profile 切换后仍逐字节可读；显式 profile 路径不被 query 覆盖。 |
| 凭据和诊断 | 不新增业务日志，不在诊断中序列化 Candidate.Source；API 继续走 httpx 原有响应策略。成功 artifact 正文和文件是管理员已认证下载的可用配置，保留必需的用户配置凭据；元数据/错误沿用共享脱敏，不另写局部替代品。 |
| 双写/失败和幂等语义 | 不新增 DB+file 双写事务；生成缓存及冻结预览交由各自已存在的原子 owner。生成失败不保存预览；重复生成产生独立候选，不污染旧预览。 |

## Flask、产物和比较器

`api/config/difftest/artifacts_test.go` 驱动真实 Flask Blueprint 与 Go HTTP pipeline：五引擎 ×
默认/显式 profile 两个前缀；响应内容、subscription hosts/dialers、配置路径/成员角色与顺序，
完整 DB/非 artifact 文件和捕获的 Source/revision 对照。每侧返回的文件大小/摘要先与其实际 ZIP
和 file 字节校验，随后按 PORTING 的配置语义跨语言比较；私有 record 的归属与 manifest
必须对应公开响应。另覆盖认证、畸形/非对象请求、缺 profile、未知服务/操作、无效/缺失/过期/
外来预览、越界/缺文件、真实生成错误及零写入。Go 原有59方法完整 server 认证/注册测试保留。

保留原 C34 已批准差异：畸形 JSON 严格400（Flask generator 捕获 HTTP 异常后500）；
非文本 base_url400且不写（Flask 可能把值隐式转成前缀）。原生 YAML 内部诊断不逐字仿 Python；
拒绝状态、错误 envelope 和持久化效果仍比较。不是本次新增兼容性豁免。

proddiff 的 `artifact_policy.py` 不盲目忽略随机字段：从本次运行的私有副本证明响应对应
record、tar manifest、归档 SHA256、全部成员大小/摘要和主配置字节，再验证 ID 格式、配对唯一性、
稳定性和一小时 TTL；仅随后替换独立 ID/时间和已证明的文件 size/hash。所有冻结成员仍由原比较器
按 YAML/JSON 结构或有序文本比较，包括 role/path/order/EOL。缺证据、坏摘要、外来 owner、到期、
同 ID 响应变化会报差异。合成负向控制包含篡改摘要、损坏/外来/过期记录及改变附加文件。
不修改全局 normalize 配置；不放宽其他 route 或错误响应。

## 验证命令与结果

执行前固定指定测试服务器的 RT_HOST；下列命令省略实际主机字面量。所有 Go 并发 `-p 2`。
最终命令及末行在交回前补齐。

首次 `gofmt`、`go vet -p 2 ./...` 通过。首次扩大到完整 deploy/api/agents 包的 race
用例集合在8分钟总上限耗尽（其中 api/agents 正在既有
`TestStatusLocalizesOldRecordsWithoutRewritingThem`）；没有作为产品缺陷或整包通过来报告。
随后仅重跑本次改动和直接调用方的相关用例，具体结果见后续记录。

没有新增依赖，没有新自制 parser/fuzz target；ZIP/tar/JSON 继续由标准库和 A14 既有校验器负责。
不运行完整门禁。HR 独立复核及批次全门禁归协调者。
