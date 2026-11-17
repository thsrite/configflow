# C07 独立复核进度

分支 go-review-c07，从作者 edde72d2b 分出。已完整读七个 Python 源文件和指定交接/复核问题类别。
不 push、不派子 agent、不碰生产；测试和 gofmt/vet 仅 rt。

已复现/修复三项：合法 ASCII 十进制分隔符计数造成 GET500/503（medium）；测试假slot/runner泄漏
（low）；listener 健康后旧状态读取失败的恢复消息（low）。三项定向回归已在 .32 通过。
新增 logs 三个500及 task四类失败的编码 stored-token 脱敏测试。完整证据见 ../review-c07.md。

变基前 .32 相关包 vet/race/live 全通过（含C35/C15/metrics）；.64 C37手动任务三轮PASS8.544s。
已无冲突代码变基到本地go@d43cca6e4（C33）；两个文档追加冲突保留双方全文。
修复提交现为bf8003685；新增真实测试在state已保存、active未切换时关机，断言保存状态/旧监听恢复，
变基后 .32 weblistener race PASS2.155s。其余C07核心包及真实Flask亦已通过，等待C35/C15/metrics尾部。
初始 .64 C07包均通过，discovery包在旧N7冻结YAML reader archive解码处长时间执行，已中止诊断，
另用短timeout/verbose定位；不计整包通过，也不将其未经定位当作C07业务回归。

进行中：.32最新集成树增量验收；.64最终gate（单机一个，900秒排队守卫），07:48:41开始等待。
下一步：补fuzz/CFG-07；等待gate结果并完成报告；不以已排队门禁冒充检查通过。
补查F4（low）：metrics slot缺失时，interval错误把 metrics_history_flush:unknown 当已知任务503；
已复现（.32 status503/want400）并在owner先校验exact key，新增真实Flask及合法缺失key503控制。
完整最新集成树定向race/live已PASS，含server2.315s、C37手动任务三轮6.249s。
07:57:17停止仍未获槽的gate（exit241，无阶段开始）补F4；恢复后仍用原08:03:41绝对排队截止，
暂停时间计入900秒，不延长。当前另在.64跑API race、60s fuzz、CFG07；.32跑F4 live/API验证。
共享repo/taskstatus最终提交窗口沿用O3-6c分派，不复制修复。CFG-07原方向Py500/Go200已核对。
