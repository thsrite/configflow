# N7-3d share — oxidns readers to yamlx

状态：ported，业务验证器保持不动；最终完整迁移计数、两个指定 fuzz 和 C29 门禁待记录。
用户从 N7-3d 分配这两个 reader 给 C29，单独提交；协调者会通知原 owner。

- native.go parseYAML 和 adapter.go safeLoad 统一 yamlx.Parse。
- 只有 ErrSyntax 保留旧 native syntax error / template fallback 契约；D1、tag/shape、merge、alias
  预算或自引用拒绝保留共享错误链，不退回宽松 reader 或默认配置。
- native 的 builder、aliasCopies、validateMerges、loadNative 大小/深度和业务 validator 不变。
  include 错误继续保留原有 source/issue 元数据，同时保留新的 reader cause。
- 旧实现只冻结在 n73_legacy_test.go，未把 Python 构造器复制到运行时。
- TestN73OxidnsReaderInputs 流式读取真实既有 fixture 的输入 args（不取预期输出）；包括
  native_config、模板 YAML 字段和 include YAML 附件。逐行比较旧/新 reader，不按旧/新业务
  输出猜测。只允许设计 §5.7 既有分类，unknown 必须为 0。
- 最终迁移门包含 include 附件和 C29 新真实参考：native 53588（equal 52768、D1 547、
  reader-policy 177、unsafe-alias 96）；template 326232（equal 325967、D1 132、unsafe-alias 133）；
  两类 unknown=0。逐输入 occurrence 计数，不去重，不取 Python 预期输出。
- 深度 40 alias/self-reference/重复键/tag-shape/无效 merge 源均拒绝；普通 syntax 才保留 fallback。
  另比对合法共享 alias/merge 的完整值，防止 reader 替换破坏原 builder 后处理。
- S 核心参考中仅“双方都作为 settings error 拒绝”的显式 yamlReadError policy/alias 诊断按 §5.7
  接受新的中文行诊断；接受的值、业务校验消息、参数 post-state 仍严格比较。不是 blanket waiver。

初始完整核心包复验发现只有新 policy/alias 诊断差异；加入上述窄范围诊断检查后，剩余两项来自
include 的结构错误包装丢失原因。现在仅补读取层 cause 保留，不改变该包装的文本或 issue/source。

所有检查通过 rt 在测试服务器执行；本机未测试、未访问生产、未 push、未开子 agent。

最终迁移计数命令（.64）：`RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -run "^TestN73OxidnsReaderInputs$" -v -timeout=15m ./internal/oxidns'`。
末行：`PASS; ok configflow/internal/oxidns 56.795s`。完整核心包较早复验：`ok ... 129.337s`。
两个指定 fuzz 与最终门禁结果将在 C29 完成时补齐。
