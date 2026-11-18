package subs

import (
	"configflow/internal/jsonresp"
	"configflow/internal/logx"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
)

var hostsLogger = logx.Get("backend.utils.subscription_hosts")

// Limits of the hosts section kept per subscription.
const (
	MaxEntries        = 1000
	MaxAddresses      = 8
	MaxSkippedSamples = 20
	MaxReportItems    = 200
)

// Reasons maps a reason code to the text shown in the preview.
var Reasons = map[string]string{
	"parse_error":        "hosts 段无法解析",
	"not_mapping":        "hosts 不是「域名: 地址」的映射",
	"wildcard":           "通配符条目不合并，只合并节点自己的域名",
	"invalid_domain":     "域名无效",
	"invalid_value":      "取值不是合法的 IP、IP 列表或域名",
	"too_many_addresses": fmt.Sprintf("IP 超过 %d 个", MaxAddresses),
	"over_limit":         fmt.Sprintf("超过每个订阅 %d 条的上限", MaxEntries),
	"not_node_domain":    "不是这个订阅里被使用节点的 server 域名",
	"custom_hosts":       "Mihomo 基础配置已手写这个域名，保留手写值",
	"mosdns_hosts":       "MosDNS 自定义 Hosts 已写这个域名，保留同步值",
	"conflict":           "另一个订阅给了不同的值，按订阅列表顺序采用排在前面的订阅",
	"cycle":              "会和已有 hosts 形成域名映射环",
}

var (
	loggedMu    sync.Mutex
	logged      = map[string]string{}
	loggedOrder []string
)

// LogHostReport logs each distinct bounded preview once per profile.
func LogHostReport(profileID string, report *HostReport) error {
	if report == nil || (report.Merged == 0 && report.Ignored == 0) {
		return nil
	}
	summary := struct {
		Merged       int `json:"merged"`
		Ignored      int `json:"ignored"`
		DomainMapped int `json:"domain_mapped"`
	}{report.Merged, report.Ignored, report.DomainMapped}
	// Only the summary and bounded details identify a log outcome.
	key := struct {
		Summary struct {
			Merged       int `json:"merged"`
			Ignored      int `json:"ignored"`
			DomainMapped int `json:"domain_mapped"`
		} `json:"summary"`
		Items []HostItem `json:"items"`
	}{summary, report.Items}
	data, err := jsonresp.Encode(key)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	loggedMu.Lock()
	if logged[profileID] == digest {
		loggedMu.Unlock()
		return nil
	}
	if _, ok := logged[profileID]; !ok {
		loggedOrder = append(loggedOrder, profileID)
	}
	logged[profileID] = digest
	for len(logged) > 64 {
		delete(logged, loggedOrder[0])
		loggedOrder = loggedOrder[1:]
	}
	loggedMu.Unlock()
	var order []string
	counts := map[string]int{}
	for _, item := range report.Items {
		if item.Status == "ignored" {
			if _, ok := counts[item.Reason]; !ok {
				order = append(order, item.Reason)
			}
			counts[item.Reason]++
		}
	}
	var parts []string
	for _, reason := range order {
		text := Reasons[reason]
		if text == "" {
			text = reason
		}
		parts = append(parts, fmt.Sprintf("%s %d 条", text, counts[reason]))
	}
	detail := strings.Join(parts, "；")
	suffix := ""
	if detail != "" {
		suffix = "（" + logx.LogText(detail, 120) + "）"
	}
	hostsLogger.Infof(msgHostsLog, report.Merged, report.DomainMapped, report.Ignored, suffix)
	return nil
}
