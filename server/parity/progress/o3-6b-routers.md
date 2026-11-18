# O3-6b — routers JobLock 与工作取消

状态：implementing。分支go-o3-6b，从协调者指定的go-opus-review-r10@f368fa1b9建立；
不push、不派子agent、本机不测试。最终需在R10及O3-6c正式合入后rebase go。

## 范围与契约

- routers拥有唯一L1 Keyed[string]，Wire接入repo.Hooks.JobLock，保持非阻塞、无repo回调。
  存在的连接按type+换行+去尾斜线endpoint；缺记录按profileID+换行+routerID。
- 当前repo对未接线JobLock默认fail-closed不放宽。接线后空闲连接可修改，发布期间仍409。
  SaveRouterSettings/SaveRouterPublication继续由调用方已持锁，不能在repo重复尝试同一L1。
- 剩余write/rollback的WithoutCancel迁到owner的lifecycle.Detach，保留请求断开完成，
  Runtime关机能取消；入口和最终设备写/本地状态提交前检查ctx，原生产I/O timeout不变。
- 不移植尚未落地的R12–R17，也不在底层driver方法重复取L1。

## R11 协调（开工前已联系694df9d0）

R11确认无临时Keyed/JobLock，无签名偏好。统一RouterJobLock(ctx,profileID,routerID,*Router)
返回held ctx/release/error，内部Detach与立即尝试；repo hook与该入口共用锁表。
R11底层PD/DNS/readiness方法已经ctxx.Detach+ctx.Err，每次设备I/O及manifest前检查；
它们没有profile/router上下文，不自行拿L1，由R12 Publish/StartPublish或A23上层取L1→L2并传ctx。
R11对RouterOS.Apply另加入口取消检查，后续rebase保留；不改R11私有产品文件。

## 步骤

1. 公共JobLock与Wire：类型/endpoint等价、缺记录fallback、冲突立即返回、释放后可用、
   L3下try无违规、旧锁文件哨兵、真实Runtime取消/断开；repo集成验证已持锁豁免与并发保护。
2. R04–R10现存write/rollback owner清除WithoutCancel，补真实Runtime回归；
   复用回环fake和生产参数，验证停止不写/不改proof、断开仍完成、L2等待可取消。
3. R10/6c合入后rebase，验证repo的真实BeforeCommit取消及并发发布互斥，保留R11集成约定。
4. .32或.64定向race、全仓vet；最终server/scripts/gate.sh（.64 -p6）。
   排队超过15分钟只停自己的排队进程并交回，由协调者cf-gate。

## 当前进度

已完成只读预检与R11接口协调。未改产品；测试待实现后在远端运行。
