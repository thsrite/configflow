# R16 独立复核

分支：`go-review-r16`；指定基底：`go-port-r16@435127b16`（作者 b6fa1f80，
基于 `go@c5f5ff34`）。本复核者不是作者。结论：R16 自有范围 verified；
high 0、medium 2（均修复）、low 0。生产 HTTP 五条路由及固定 IP 属 R17，
没有把测试适配器当作正式路由通过证明。未运行完整门禁、未访问生产、未 push。

## 发现与修复

### M1：发现元数据可引用其他设备/Agent 的凭据

- 场景：路由器错误/备注/hostname 引用另一路由器密码或配置内已知 Agent token；
  保存的设备名称及路由器名称也可含此引用。原实现直接返回这些文本，单驱动
  只遮盖自己的密码，缓存刷新失败的诊断及 unsupported 分支同样绕过防护。
- 证据：`internal/clients/review_r16_test.go` 的
  `TestReviewR16DiscoveryCredentialReferences` 四个子例在修复前全部失败。
  `TestReviewR16RealDriverPeerCredential` 用真实 RouterOS 驱动连接合成 loopback
  服务，验证一个路由器的 400 detail 引用另一个密码时，健康路由器及客户端仍可见。
- 修复：`DiscoveryService.Discover` 在完整响应投影之后调用共享 redact，凭据种子
  从原始配置收集，覆盖 URL 编码引用、sources、devices/discovered 与 refresh_error。
  不改私有输入、缓存及乐观更新依据；正常用户名保留。无私有 redaction 实现。
- 修复提交：本报告首次随附的产品修复提交。

### M2：RouterOS 混合列表中的坏行隐藏健康行

- 场景：ARP、地址、DHCP server 或 lease 数组混入 null/布尔/字符串。
  原 `clientList` 丢弃整表：必需 ARP 导致发现 failed；DHCP server 失效时可能连
  LAN 都无法确定；可选地址/租约里的健康证据也丢失。Python 有同类缺陷。
- 证据：`TestReviewR16MalformedRowsRetainHealthyClients` 修复前四个表子例均失败，
  分别表现为整次失败或健康地址/租约证据丢失；修复后均通过。
- 修复：只在 R16 的只读表 helper 中跳过非对象行，并传递 complete=false。
  保留正常客户端、路由器自身地址和租约名称；未出现的已保存客户端为 unknown。
  未放宽共有写操作或所有权校验。`TestReviewR16InvalidARPContainerRemainsFailure`
  是负对照：整个回复为 null/对象/布尔时仍 failed，不能伪装成成功空扫描。
- 修复提交：本报告首次随附的产品修复提交。

两项行为变化已登记 `behaviour-changes.md`。

## 核查结论与后续边界

- 已完整阅读 R16 五个 Python 文件、Go 实现和作者测试；FILES 中遗漏的两个
  discovery 模块归属补为 R16。Step B 无直接归 R16 的未解决条目；UDP 协议引用
  冲突明确属 C26/R17，本分支不重复修改该共享实现。
- HTTP/存储抽查：作者真实 Flask 对照的单行修改流程（名称、策略、冲突409、
  地址/名称/模式错误400、HEAD）及 RouterOS/iKuai 正常发现、必需表失败、
  可选租约失败通过；比较状态、业务字段、Cache-Control、DB 全字段/revision 和用户文件。
  只执行这些边界样例，没有重跑作者整套 Python/Go 测试或 fuzz。
- `TestReviewR16CancellationAtRepositoryCommit` 用真实 Runtime.Start/BindRequest/Shutdown，
  在 UpdateDevice 已产生替换文档、事务回调即将返回时关机：context.Canceled，
  DB、revision、用户文件不变；同一提交点断连的正对照写入成功且 revision+1。
  原 owner/cache 的真实 Runtime 断连、关机测试也抽查通过。
- `TestReviewR16RouterRecordsAndIkuaiNulls` 验证 routers.items 的坏项只影响自己，
  name/type null 和错误类型成员遵循已有 R5 fallback；iKuai 三表 data:null 不拖垮
  健康来源，混合列表坏行被跳过。保存的名字/策略仍沿用 C26/C31 的显式校验，
  本复核没有放宽请求验证或覆盖坏成员的原始存储。
- 缓存按 profile 和包含凭据的完整 router 设置摘要隔离；旧 generation 不能回填，
  saved names/AgentHosts/GatewayHost 每次重新合并，关闭驱动有 defer，设备 I/O 有期限。
- R17 继续负责：正式路由的状态映射、auth/profile/Agent pinning；HEAD 走 GET，
  不进入保存/发布事务；AgentHosts 必须按当前 profile 过滤；GatewayHost 接 A01；
  Enrich 使用同一 reader/reading，只禁用失败的固定 IP 控件。ListDevices 是原始
  草稿视图，也是乐观更新的输入，R17 的公开列表需应用完整配置凭据脱敏及全局
  repository token hook；不能直接依赖单设备 driver 的密码过滤。

## 定向验证

主机为用户指定的测试服务器；下面 `$R16_TEST_HOST` 固定为该 SSH 目标。
所有夹具合成，网络只连 loopback。完整门禁由协调者在集成批次执行。

1. 修复前：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/clients -run "^TestReviewR16"'
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/clients -run "^TestReviewR16MalformedRows"'
   ```

   第一条暴露四个凭据分支及必需表失败；加强地址/租约证据断言后第二条四表全失败。

2. 修复后与抽查：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/clients -run "^(TestReviewR16|TestRepositoryDeviceWriteRespectsRuntime|TestOwnersDetachDisconnectAndRejectShutdown|TestFlaskDeviceWritesAndStoredState)" && go test -race -count=1 ./internal/clients -run "^TestFlaskDiscoveryRealDrivers$/^(routeros|ikuai|arp_required|lease_degraded)$" && go test -race -count=1 ./internal/routers -run "^(TestClient|TestR13ReviewMalformedRouterIsolated$)" && test -z "$(gofmt -l internal/clients internal/routers/clients_ros.go)" && go vet ./...'
   ```

   退出0：clients `6.217s`、Flask/driver 抽查 `7.466s`、routers `5.943s`；
   gofmt 无输出，`go vet ./...` 通过。

3. 新增提交点回归（作者用例没有覆盖“已计算替换文档后再取消”）：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 -v ./internal/clients -run "^TestReviewR16CancellationAtRepositoryCommit$" && test -z "$(gofmt -l internal/clients internal/routers/clients_ros.go)" && go vet ./...'
   ```

   disconnect/shutdown 两子例 PASS，clients `2.303s`；gofmt/vet 退出0。

4. 最终全部复核用例复跑：

   ```sh
   RT_HOST="$R16_TEST_HOST" rt -- bash -lc 'cd server && go test -race -count=1 ./internal/clients -run "^TestReviewR16" && go vet ./... && test -z "$(gofmt -l internal/clients internal/routers/clients_ros.go)"'
   ```

   退出0：clients `2.036s`；vet/gofmt 通过。随后将编码夹具改为确实需要
   URL 编码的合成密码，并增加夹具负对照；单独复跑
   `-run "^TestReviewR16DiscoveryCredentialReferences$"` 通过（`1.132s`）。
