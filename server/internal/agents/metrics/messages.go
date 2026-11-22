package metrics

const msgSavedMetricsArray = "监控历史 metrics 字段必须是数组"

const msgTrafficSettingsShape = "流量记录的系统配置格式不正确，请修复后重试。"

const msgTrafficRetention = "保留时长只能是 30、90、180 或 365 天"

const msgFlushInterval = "保存间隔只能是 5、10、30 或 60 分钟"
const msgFlushTask = "监控历史保存"
const msgFlushDetail = "Agent 系统监控（CPU、内存、网速） · Mihomo 流量记录"
const msgCorruptMetric = "监控历史字段损坏，已跳过："

const msgHoursOutOfRange = "查询时长超出可表示的日期范围"

const msgTrafficCounterRange = "流量计数超出可保存的范围"

const msgTrafficCounterInvalid = "流量记录里有无效的计数"
