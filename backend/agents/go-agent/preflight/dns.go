package preflight

import (
	"context"
	"fmt"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// DNSCandidate checks the final rendered configuration without replacing the
// live file. ConfigPath must remain the live path to identify its running core.
type DNSCandidate struct {
	Config  Config
	Content []byte
}
type DNSOptions struct {
	Peer      *DNSCandidate
	Probe     bool
	QueryName string
}

type dnsQueryProbe interface {
	Query(context.Context, portBinding, string) error
}

// CheckDNS is a read-only publication check. It deliberately excludes Agent
// ports, TUN device permissions and routing requirements; installers retain
// their independent environment checks.
func CheckDNS(candidate DNSCandidate, options DNSOptions) Report {
	return checkDNSWithProbe(candidate, options, procPortProbe{Root: "/proc"}, wireDNSProbe{}, runtime.GOOS)
}

func dnsDocument(candidate DNSCandidate) (map[string]interface{}, error) {
	var doc map[string]interface{}
	if err := yaml.Unmarshal(candidate.Content, &doc); err != nil || len(doc) == 0 {
		return nil, fmt.Errorf("DNS 检测需要有效、非空的 YAML 配置")
	}
	return doc, nil
}

func dnsRequirements(cfg Config, doc map[string]interface{}, index int) ([]portRequirement, []Check) {
	// Candidate and peer files are checked as supplied, including custom ports.
	requirements, checks := corePortRequirements(cfg, doc, index)
	for i := range checks {
		checks[i].Fix = "检查实际配置的 DNS 监听地址、端口及外部 include 文件后重新检测。"
	}
	return requirements, checks
}

func checkDNSWithProbe(candidate DNSCandidate, options DNSOptions, ports portProbe, query dnsQueryProbe, platform string) Report {
	cfg := candidate.Config
	if cfg.ServiceType != "mihomo" && cfg.ServiceType != "oxidns" && cfg.ServiceType != "mosdns" {
		return Report{{Name: "DNS 配置", Level: "warn", Message: "该核心类型尚不支持 DNS 检测"}}
	}
	doc, err := dnsDocument(candidate)
	if err != nil {
		return Report{{Name: "DNS 配置", Level: "error", Message: err.Error()}}
	}
	requirements, checks := dnsRequirements(cfg, doc, 0)
	report := Report(checks)
	report = append(report, dnsSelfChecks(cfg, doc, requirements)...)
	if cfg.NativeAIO {
		report = append(report, dnsHijackChecks(cfg, doc)...)
		if options.Peer != nil {
			peerDoc, peerErr := dnsDocument(*options.Peer)
			if peerErr != nil {
				report = append(report, Check{Name: "DNS 上下游", Level: "warn", Message: "无法读取有效的对端配置，上下游关系尚未验证"})
			} else {
				peerRequirements, peerChecks := dnsRequirements(options.Peer.Config, peerDoc, 1)
				if Report(peerChecks).HasErrors() {
					report = append(report, Check{Name: "DNS 上下游", Level: "warn", Message: "对端 DNS 监听配置无法完整识别，上下游关系尚未验证"})
				} else {
					for _, a := range requirements {
						for _, b := range peerRequirements {
							if portBindingsOverlap(a.portBinding, b.portBinding) {
								report = append(report, Check{Name: "DNS 监听冲突", Level: "error", Message: fmt.Sprintf("%s 与 %s 的 %s/%d 监听地址重叠", cfg.ServiceType, options.Peer.Config.ServiceType, a.Protocol, a.Port), Fix: "为两套 DNS 配置不同的监听地址或端口，并同步更新上游地址。"})
							}
						}
					}
					report = append(report, dnsRelationshipChecks(candidate, doc, requirements, *options.Peer, peerDoc, peerRequirements)...)
					report = append(report, dnsTUNUpstreamChecks(cfg, doc, options.Peer.Config, peerDoc)...)
				}
			}
		}
	}
	for i, a := range requirements {
		for _, b := range requirements[:i] {
			if portBindingsOverlap(a.portBinding, b.portBinding) {
				report = append(report, Check{Name: "DNS 监听冲突", Level: "error", Message: fmt.Sprintf("待发布配置包含重叠的 %s/%d DNS 监听", a.Protocol, a.Port), Fix: "删除重复监听或使用不重叠的地址和端口。"})
			}
		}
	}
	if len(requirements) == 0 {
		if !Report(checks).HasErrors() && len(checks) == 0 {
			report = append(report, Check{Name: "DNS 监听", Level: "pass", Message: "不适用：未启用外部 DNS 监听"})
		}
		return report
	}
	if platform != "linux" {
		return append(report, Check{Name: "DNS 端口占用", Level: "warn", Message: "当前平台无法核实 DNS 监听进程归属，端口占用和实际解析尚未验证"})
	}
	listeners, err := ports.Listeners()
	if err != nil {
		return append(report, Check{Name: "DNS 端口占用", Level: "warn", Message: "无法读取网络监听或进程信息，端口占用和实际解析尚未验证", Fix: "在目标设备检查 /proc 可读性，并用 ss -lntup 核对 DNS 监听进程。"})
	}
	binary := cfg.ServiceBinary
	if binary == "" {
		binary = cfg.ServiceType
	}
	executable, _ := ports.Executable(binary)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	probed := map[portBinding]bool{}
	for _, requirement := range requirements {
		check := Check{Name: fmt.Sprintf("%s DNS %s/%d", cfg.ServiceType, requirement.Protocol, requirement.Port), Level: "pass", Message: "当前没有重叠的监听占用"}
		owned, unknown, external := false, false, false
		var live []portBinding
		var pids []string
		for _, listener := range listeners {
			if !portBindingsOverlap(requirement.portBinding, listener.portBinding) {
				continue
			}
			if listener.Uncertain || len(listener.Owners) == 0 {
				unknown = true
			}
			for _, owner := range listener.Owners {
				pids = append(pids, strconv.Itoa(owner.PID))
				if owner.Uncertain {
					unknown = true
					continue
				}
				if portOwnerIsCore(owner, cfg, executable) {
					owned = true
					live = append(live, listener.portBinding)
				} else {
					external = true
				}
			}
		}
		if external || unknown {
			check.Level = "error"
			check.Message = "DNS 监听被其他进程占用"
			if unknown {
				check.Message = "DNS 监听已有占用，但无法核实全部进程归属"
			}
			if len(pids) > 0 {
				check.Message += "（PID " + strings.Join(pids, ", ") + "）"
			}
			check.Fix = "用 ss -lntup 确认占用程序后调整 DNS 监听；检测不会停止任何进程。"
		} else if owned {
			check.Message = "监听属于当前受管核心，可继续发布配置"
		}
		report = append(report, check)
		if !options.Probe || external || unknown {
			continue
		}
		if !owned {
			report = append(report, Check{Name: "DNS 解析待生效", Level: "warn", Message: fmt.Sprintf("%s/%d 尚未由当前核心监听；新配置生效后才能验证解析", requirement.Protocol, requirement.Port)})
			continue
		}
		for _, binding := range live {
			if probed[binding] {
				continue
			}
			probed[binding] = true
			target := dnsProbeBinding(binding)
			name := options.QueryName
			if name == "" {
				name = "example.com"
			}
			check := Check{Name: fmt.Sprintf("DNS 当前解析 %s/%d", binding.Protocol, binding.Port), Level: "pass", Message: "当前受管监听解析正常；此结果不代表待发布配置已生效"}
			if len(probed) > 8 || ctx.Err() != nil {
				check.Level = "warn"
				check.Message = "检测时间或监听数量达到上限，部分监听解析尚未验证"
			} else if query.Query(ctx, target, name) != nil {
				check.Level = "warn"
				check.Message = "当前受管监听的 DNS 查询失败、超时或未返回答案；不阻止发布修复配置"
				check.Fix = "核对 DNS 上游地址、网络及分流规则；新配置生效后重新验证。"
			}
			report = append(report, check)
		}
	}
	return report
}

func dnsProbeBinding(binding portBinding) portBinding {
	if !binding.Address.IsValid() || binding.Address.IsUnspecified() {
		if binding.Address.Is6() {
			binding.Address = netip.IPv6Loopback()
		} else {
			binding.Address = netip.MustParseAddr("127.0.0.1")
		}
	}
	return binding
}

func dnsHijackChecks(cfg Config, doc map[string]interface{}) []Check {
	if cfg.ServiceType != "mihomo" {
		return nil
	}
	var tunnels []map[string]interface{}
	tun, _ := doc["tun"].(map[string]interface{})
	if enabled, _ := tun["enable"].(bool); enabled {
		tunnels = append(tunnels, tun)
	}
	listeners, _ := doc["listeners"].([]interface{})
	for _, item := range listeners {
		listener, _ := item.(map[string]interface{})
		kind, _ := listener["type"].(string)
		disabled, _ := listener["disabled"].(bool)
		if kind == "tun" && !disabled {
			tunnels = append(tunnels, listener)
		}
	}
	for _, tunnel := range tunnels {
		hijacks, _ := tunnel["dns-hijack"].([]interface{})
		for _, item := range hijacks {
			value, _ := item.(string)
			if value == "any:53" || value == "tcp://any:53" || value == "udp://any:53" {
				return []Check{{Name: "DNS 劫持范围", Level: "warn", Message: "一体化模式下，TUN 对目标端口 53 的 DNS 劫持可能绕过同机 DNS 服务；这不是监听端口占用", Fix: "确认进入 TUN 的 DNS 请求是否应先交给同机 DNS 服务，并核对实际路由和排除规则。"}}
			}
		}
	}
	return nil
}

// Mihomo adds these exclusions when it is published; an OxiDNS publication can
// only report upstreams that the running Mihomo would still answer itself.
func dnsTUNUpstreamChecks(cfg Config, doc map[string]interface{}, peer Config, peerDoc map[string]interface{}) []Check {
	if cfg.ServiceType != "oxidns" || peer.ServiceType != "mihomo" {
		return nil
	}
	tun, _ := peerDoc["tun"].(map[string]interface{})
	if tun["enable"] != true {
		return nil
	}
	excluded, _ := tun["route-exclude-address"].([]interface{})
	var missing []string
	for _, address := range TUNHijackedOxiDNSUpstreams(doc, tun) {
		if !PrefixesCover(excluded, address) {
			missing = append(missing, address.String())
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []Check{{Name: "同机 TUN 劫持上游", Level: "warn",
		Message: fmt.Sprintf("同机 Mihomo 的 TUN 会劫持发往 %s 的 53 端口查询并返回 fake-ip，OxiDNS 拿不到这些上游的真实解析", strings.Join(missing, "、")),
		Fix:     "OxiDNS 发布后再发布一次同机 Mihomo，Agent 会把这些地址加入 tun.route-exclude-address 并重载 OxiDNS；若仍返回 fake-ip，再发布一次 OxiDNS。"}}
}
