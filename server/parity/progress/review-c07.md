# C07 独立复核进度

状态 verified：0 high / 1 medium / 3 low，四项均修复，0个模块内未决发现。
分支 go-review-c07，从作者 edde72d2b 分出，最终基底本地go@d43cca6e4（含C33复核）。
七个Python源文件已全文读取；C06/C33缺失报告从各自复核分支补读，变基保留双方文档追加。
不 push、不派子 agent、不碰生产；测试/gofmt/vet一律rt，新增输入全部合成。

修复：bf8003685 合法旧计数分隔符、假slot/runner清理、listener回滚提示；
ae816cb07 unknown exact task key在readiness前返回400；e32998805补真实两次listener提交间关机回滚。
每项都有修复前失败及修复后回归，详见 ../review-c07.md 的场景、证据、命令和输出。

.32最新集成树全树格式/vet、相关包race/live（含C35/C15/metrics/server）通过；
C37手动任务真实Runtime三轮在.32/.64分别6.249s/8.544s通过。
F4追加完整API race及Flask差分通过；exact-key400/合法缺失key503的正负控race count3通过。
.64 counter fuzz60s PASS686577 execs；两台CFG07均Py/Go500/500、compared1/diffs0/open0，
155比较器自检和端到端mutation check通过；最终报告标注ae816cb07，未改全局结果。

最终gate在.64前台、单agent slot1、Python守卫运行。07:48:41原始排队；07:57:17停止未获槽的
首轮补F4；08:00:09恢复但保留原08:03:41绝对截止（含暂停900秒）。最终exit75，无任何阶段开始。
日志~/.cache/rt-tmp/review-c07-final-resumed-20261118；不计GATE PASS，无本轮proddiff汇总。
协调者在合并树跑cf-gate；不延长队列或在gate外跑全量race/proddiff。

诊断不计通过：discovery整包的N7冻结YAML语料校验很慢，SIGQUIT停止及90s超时均已如实记入报告；
未改或跳过它，不将其未经确认报为C07业务缺陷。仅C37手动任务三轮明确通过。
共享repo/taskstatus最终提交窗口仍归O3-6c；后合入方接RecordContext/ForgetContext。
R14/A16/A17 rows/runner、C06 handler和routers confirmed-empty apply仍归原owner，无生产假slot。
