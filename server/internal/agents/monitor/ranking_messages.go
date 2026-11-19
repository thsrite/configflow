package monitor

import (
	"fmt"
	"strings"
)

func rankingReason(code string) string {
	reasons := map[string]string{
		"upgrade_required":          "请将 Agent 升级到 " + MinDashboardAgentVersion + " 或更新版本以查看排行并重置统计。",
		"recorder_not_configured":   "未启用查询记录，无法计算域名和客户端排行。",
		"unsupported_configuration": "当前配置未包含可确认的拦截路径（标准 AdGuard 或规则「拒绝/黑洞」）。",
		"too_many_block_rules": fmt.Sprintf("拦截规则分组超过 %d 个，暂不统计被拦截域名排行；", BlockRankingMatcherLimit) +
			"请合并拒绝/黑洞规则后再查看。",
		"snapshot_changed": "采集期间核心或配置发生变化，请稍后刷新。",
		"rankings_paused":  fmt.Sprintf("上次统计排行超时，已暂停 %d 分钟后再试，以免影响查询记录。", rankingBackoffMinutes()),
	}
	reason, ok := reasons[code]
	if !ok {
		if reason, ok = Messages[strings.ToUpper(code)]; !ok {
			reason = "暂时无法获取排行，请稍后刷新。"
		}
	}
	return reason
}
