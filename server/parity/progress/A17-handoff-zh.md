# A17 DNS follow 交回材料（进行中）

当前状态：partial，尚未交回。分支 go-port-a17，从 go-opus-review-a14@b6ce4ac29 起步；A14尚未合入go，按协调者要求待其合入后再变基。只用合成数据及回环Agent，不push、不创建子agent；按Leaner verification只做定向验证。

已读：A17 PROGRESS、A14复核及HANDOFF、C26覆盖核对、O3-6锁/owner说明、AGENT-BRIEF、最新go的WORKFLOW Leaner verification、A16–A18设计的A17/共享接口部分；Python dns_follow.py及四条路由、test_dns_follow.py完整分支（70测试函数/参数化）。D-A17-1已裁定B：沿用Go-native CanonicalDigest，切换后已有binding需重新选择一次；不仿真Python JSON编码。

已通过Paseo与A16（0d31fb93）对齐：A17不依赖rulesync设备FetchSnapshot；复用A14 Source.Raw/Member/WithMember、SourceFromJSON、CanonicalDigest、PrepareContent/Publish及model.Agent/DNSFollow*。A16负责RuleSyncRow/PinnedWithheld及HAEnabled/HAPair导出；A17负责DNSFollowRow/runner。api/agents各用独立文件，注册/server/routes合并保留双方接线。

已实现：internal/agents/dnsfollow的绑定/解除/重试、options投影、C26文档/来源检查、IdleRounds、状态/事件去重、child reservation/job/publish/resume、profile CAS及ownership复检；四条auth路由；C07 SetDNSFollowRow/SetRunner；boot periodic。只有adapter.go接触jsonx；不增加锁文件或裸goroutine。Wire(m)初始化manager owner，Register()是server的无参数组合入口。旧manager关闭后从pool移除，L1 keyed锁按数据目录共享。

初轮定向：.64经rt，gofmt；go test -race -count=1 ./internal/agents/dnsfollow，通过51.915s（两条真实SQLite/回环Agent/A14 prepare/publish用例）：只改选定上游且重复tick无重复上传；已验证未变地址不prepare/download。API/server组合编译通过（-run ^$）。新fixture初版误用只允许元数据的UpdateProfile、以及将fake的202/verifying误作同步终态，已分别改为真实UpdateProfileTransaction和真实GetDeployment确认，不改生产参数。

待完成：真实Python HTTP/状态/文件对照；空闲轮次commit计数；丢响应/重启/并发所有权及漂移、泄露、绑定/任务路由边界；写owner真实Runtime取消/断连回归；定向格式/vet/race及必要负控；A14进go后的最终变基与共享Context入口衔接。当前parent仍有已分派O3-6c的共享提交窗口，不能用本模块测试声称该窗口已修；合并时使用RecordContext传owner ctx。

完成后补齐：Python测试覆盖映射、差异清单、精确命令及最终输出、修复/交回提交和已知依赖。独立Opus HR及完整批次门禁由协调者安排。
