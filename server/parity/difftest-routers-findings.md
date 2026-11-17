# Routers Step B 黑盒差分发现

状态：进行中。矩阵审计尚未完成；不能据此宣称 routers 已通过验收。

矩阵：26 条路由、2301 个设计条目。
公开 Go 入口实际注册本领域路由：0 条。
执行配方统计：`{"go-unimplemented": 1514}`。
已有执行配方引用的设计条目：474；逐条变体审计仍未完成。

“Go 尚未实现”单独计数，不算失败，也不算差分通过。路由层 R15/R17 未注册时，
即使底层 R01–R06 已移植，也不能声称 HTTP 行为已比较。

仅使用合成数据；两端独立 DATA_DIR、网络命名空间和设备 fake；未访问生产或真实设备。
比较器会检查状态、语义响应头、类型/键/值/数组顺序、完整逻辑数据库、文件与设备状态。
Python 前置条件失败单列为夹具错误，不算 Go 发现；实际比较为零时不能下“无缺陷”结论。

目前没有已比较的 Go 缺陷结论。Python 参考异常单独记录；“Go 尚未实现”不算差分通过。

## Python bug · medium · IKUAI_NETMASK_SUFFIX_ROLLBACK

- 用例：R011-S02/real-suffix-python-bug；矩阵：R011-S02
- 建议负责模块：R05（iKuai）。
- 默认 fake 保留真实 static_rt netmask 显示后缀。Python 发布返回 502，
  新增静态路由的回读和补偿均因后缀不匹配失败，设备留下配置。未修改 fake 来掩盖此问题。
- 请求：`{"method": "POST", "path": "/api/routers/r-ik/apply", "document": {"fingerprint": "bfba4a58e875bf6e"}, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 502, "headers": {"access-control-allow-origin": ["*"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "json", "body": {"message": "阶段「routes」失败，回滚未完成，请检查发布记录", "plan": {"blockers": [], "checks": {"gateway": "10.44.0.2", "ikuai_groups": {"nat_bypass": {"groups": {"cf_nat_1": "a2c2c8777b491796"}, "removed": [], "rules": [{"comment": "cf-mihomo", "func": "nat_rule", "groups": ["cf_nat_1"], "id": null, "keep_foreign": true}]}, "routes": {"groups": {}, "removed": [], "rules": []}}, "nat_bypass": ["198.18.0.0/16"], "nat_not_required": [], "removed_routes": [], "routes": [{"cidr": "198.18.0.0/16", "gateway": "10.44.0.2"}]}, "empty": false, "fingerprint": "bfba4a58e875bf6e", "script": "", "stages": [{"enabled": false, "key": "return_routing", "manual": false, "note": "未启用自动配置回流：本次跳过，已有回流规则保留，不新增、修改或删除。", "ops": [], "title": "mihomo 回流规则"}, {"key": "routes", "manual": false, "ops": [{"cidr": "198.18.0.0/16", "comment": "cf-fake-ip", "gateway": "10.44.0.2", "kind": "route_add", "summary": "新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）"}], "title": "主路由代理路由"}, {"key": "nat_bypass", "manual": false, "ops": [{"comment": "cf-mihomo-nat", "kind": "ikuai_group_add", "name": "cf_nat_1", "role": "nat", "summary": "新建 IP 分组「cf_nat_1」（NAT 免转换使用，1 个网段）", "values": ["198.18.0.0/16"]}, {"comment": "cf-mihomo", "dst": ["198.18.0.0/16"], "groups": ["cf_nat_1"], "kind": "nat_add", "src": ["10.44.0.1-10.44.0.1", "10.44.0.3-10.44.0.254"], "summary": "新增 NAT 免转换规则：源 10.44.0.1-10.44.0.1, 10.44.0.3-10.44.0.254，目的 IP 分组 cf_nat_1（共 1 个网段）"}], "title": "主路由 NAT 免转换"}, {"enabled": false, "key": "dhcp_dns", "manual": false, "note": "未选择 DHCP DNS 服务器：本次保留现有 DNS 配置。", "ops": [], "title": "DHCP DNS"}], "warnings": []}, "result": {"failed_stage": "routes", "log": [{"stage": "routes", "status": "ok", "summary": "新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）"}, {"stage": "routes", "status": "failed", "summary": "iKuai static_rt 已提交但读不回新对象：198.18.0.0/16"}], "ok": false, "rollback": [{"stage": "routes", "status": "failed", "summary": "撤销失败：新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）（iKuai static_rt 中名称为 cf_rt_a2c2c8777 的对象已被修改，未自动删除，请检查）"}]}, "success": false}}`
- Python 设备终态：`{"created_static_rows_remaining": 1, "static_rt_after": [{"id": 2, "enabled": "yes", "tagname": "cf_rt_a2c2c8777", "ip_type": "4", "interface": "auto", "dst_addr": "198.18.0.0", "gateway": "10.44.0.2", "netmask": "255.255.0.0", "prio": 1, "comment": "cf-fake-ip"}]}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查 R05）：`{"missing_routes": ["R011", "R012"], "prior_missing_mutations": []}`
- 普通 netmask 的独立变体覆盖 R011-S02 成功路径；默认后缀变体记录这个 Python bug。
- L5 待核：真实固件 static_rt 的 netmask 后缀形态，以及失败后设备补偿是否干净。

后续仍须补齐：其余 H 公开可达性与复合 guard 变体、设备失败/补偿、完整生成形态、
Agent/备份/代理、后台轮询、断连/重启、动态值内容绑定和 O12 分类。详见进度文件与执行 ledger。
