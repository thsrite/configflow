# C07 独立复核进度

分支 go-review-c07，从作者 edde72d2b 分出。已完整读七个 Python 源文件和指定交接/复核问题类别。
不 push、不派子 agent、不碰生产；测试和 gofmt/vet 仅 rt。

已复现/修复三项：合法 ASCII 十进制分隔符计数造成 GET500/503（medium）；测试假slot/runner泄漏
（low）；listener 健康后旧状态读取失败的恢复消息（low）。三项定向回归已在 .32 通过。
新增 logs 三个500及 task四类失败的编码 stored-token 脱敏测试。完整证据见 ../review-c07.md。

进行中：.32相关包 vet/race/live；.64 C37手动任务三轮 Runtime 验证。
初始 .64 C07包均通过，discovery包在旧N7冻结YAML reader archive解码处长时间执行，已中止诊断，
另用短timeout/verbose定位；不计整包通过，也不将其未经定位当作C07业务回归。

下一步：完成定向测试；提交；rebase最新本地go；再跑影响包和最终gate（单机一个，排队900秒即停）。
共享repo/taskstatus最终提交窗口沿用O3-6c分派，不复制修复。CFG-07原方向Py500/Go200已核对。
