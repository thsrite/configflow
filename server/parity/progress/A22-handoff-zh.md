# A22 网关安装交接（进行中）

分支 `go-port-a22`，原基线 A21 `26a8a649c`。本检查点实现私有持久会话、创建/复用/短链接、限定注册、原子双凭据绑定、网络确认、顺序发布与恢复、七方法 HTTP 及 C36 安装 owner 接线。无 push、无子 agent、无设备访问。合成夹具，定向检查固定测试服务器。

已完成：`go test -count=1 -timeout=3m ./internal/gateway/installation` 通过（26.988s）；三个调用包编译通过。使用真实 installer/onboarding/deploy/host，不复制生成器或发布器。登记凭据恢复在 registry 的同一事务中证明 capability marker 与完整身份。

进行中：按协调者通知用 `git rebase --onto go-opus-review-a21 26a8a649c` 替换基线；对接复核 F1 的 400 分类及 F4 的两级脱敏。继续补真实 Flask HTTP、发布/配置产物对照和 C36 server 注册测试；再运行定向 race、vet、fuzz，最终标 ported。

当前代码已编译且自有测试通过；无半写文件。下一步优先补齐边界证据，再整理此交接。
