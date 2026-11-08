// Package preflight inspects the local environment before native Agent installation.
// Run is read-only. Explicit PrepareTUN may load the driver and persist module
// loading, but neither operation starts cores, binds ports or creates interfaces.
package preflight

import (
	"agent/clioutput"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Config contains only the installation metadata needed for environment checks.
// Registration credentials are deliberately not decoded or printed.
type Config struct {
	AgentConfigFile  string `json:"-"`
	AgentName        string `json:"agent_name"`
	AgentHost        string `json:"agent_host"`
	AgentPort        int    `json:"agent_port"`
	AgentServiceUnit string `json:"agent_service_unit"`
	ServiceType      string `json:"service_type"`
	ServiceManager   string `json:"service_manager"`
	ServiceUnit      string `json:"service_unit"`
	ServiceBinary    string `json:"service_binary"`
	ConfigPath       string `json:"config_path"`
	NativeAIO        bool   `json:"native_aio"`
}

type Check struct {
	Name    string
	Level   string // pass, warn, error, or info (logged only, never counted)
	Message string
	Fix     string
	// Groups hold exact, copy-pasteable commands per source; they are printed
	// unwrapped, each with its own label and note.
	Groups []CommandGroup `json:",omitempty"`
}

type CommandGroup struct {
	Label    string
	Commands []string
	Note     string `json:",omitempty"`
}

func (group CommandGroup) heading() string {
	if group.Label == "" {
		return "在这台 Linux 上执行："
	}
	return "在这台 Linux 上执行（" + group.Label + "）："
}

type Report []Check

func (r Report) HasErrors() bool {
	for _, check := range r {
		if check.Level == "error" {
			return true
		}
	}
	return false
}

// Print keeps values on one line, so a filename cannot forge another result or
// inject terminal control sequences. Repair instructions are explanatory only.
func (r Report) Print(out io.Writer) {
	r.print(out, false)
}

// PrintPreparation deliberately distinguishes the opt-in installer mutation
// from the read-only environment report.
func (r Report) PrintPreparation(out io.Writer) {
	r.print(out, true)
}

// PrintWithOptions separates complete evidence from the short operator-facing
// report. It does not run checks, mutate their outcomes, or change exit status.
func (r Report) PrintWithOptions(out io.Writer, options clioutput.Options, preparation bool) {
	if options.Details != nil {
		r.print(options.Details, preparation)
	}
	if options.Verbose {
		r.printDetailed(out, preparation, !options.Nested)
		return
	}
	if !options.Nested {
		if preparation {
			fmt.Fprint(out, "\nConfigFlow · TUN 安装准备\n\n")
		} else {
			fmt.Fprint(out, "\nConfigFlow · 环境检查（只读）\n\n")
		}
	}
	passed, warnings, failures := 0, 0, 0
	for _, check := range r {
		switch check.Level {
		case "info":
			continue
		case "warn":
			warnings++
		case "error":
			failures++
		default:
			passed++
			if summary := compactPass(check); summary != "" {
				clioutput.Row(out, options, "pass", summary)
			}
			continue
		}
		fmt.Fprintln(out)
		clioutput.Row(out, options, check.Level, check.Name)
		clioutput.Wrap(out, "    原因：", check.Message, options.Width)
		if check.Fix != "" {
			clioutput.Wrap(out, "    处理方法：", check.Fix, options.Width)
		}
		for _, group := range check.Groups {
			// Wrapping would break a command that is meant to be pasted.
			fmt.Fprintln(out, "    "+clioutput.Printable(group.heading()))
			for _, command := range group.Commands {
				fmt.Fprintln(out, "      "+clioutput.Printable(command))
			}
			if group.Note != "" {
				clioutput.Wrap(out, "      ", group.Note, options.Width)
			}
		}
	}
	fmt.Fprintln(out)
	operation, completed := "环境检查", "通过"
	if preparation {
		operation, completed = "TUN 准备", "完成"
	}
	level, summary := "pass", fmt.Sprintf("%s%s · %d 项通过", operation, completed, passed)
	if failures > 0 {
		level = "error"
		summary = fmt.Sprintf("%s未通过 · %d 项错误，%d 项提醒，%d 项通过", operation, failures, warnings, passed)
	} else if warnings > 0 {
		level = "warn"
		summary = fmt.Sprintf("%s完成 · %d 项通过，%d 项提醒", operation, passed, warnings)
	}
	clioutput.Row(out, options, level, summary)
	if !options.Nested && !preparation {
		clioutput.Wrap(out, "  范围：", "当前环境与接口归属；未验证流量转发或重启后的状态。", options.Width)
	}
}

var coreCapEffSummary = regexp.MustCompile(`持有有效 CAP_NET_ADMIN（PID ([0-9]+)，CapEff=([0-9a-f]+)）`)

var tunRuntimeSummary = regexp.MustCompile(`Mihomo 核心 PID ([0-9]+) 正持有内核 TUN/TAP 接口 (.+?)，`)

// Keep just the facts useful at a glance. All permission checks, port evidence,
// and qualifications remain in the private log and verbose report.
func compactPass(check Check) string {
	switch check.Name {
	case "TUN 实际启动":
		if match := tunRuntimeSummary.FindStringSubmatch(check.Message); len(match) == 3 {
			return "TUN  " + match[2] + " · Mihomo PID " + match[1] + " 已持有"
		}
		return "TUN  " + firstClause(check.Message)
	case "IPv4 转发":
		return "IPv4 转发  已开启"
	case "IPv4 转发持久化":
		return "IPv4 转发  已找到持久化开启设置"
	case installerForwardingName:
		if strings.Contains(check.Message, "ip_forward=0") {
			return "IPv4 转发  当前为 0，安装器将开启并持久化（" + installerForwardingConfig + "）"
		}
		return "IPv4 转发  当前为 1，安装器将持久化（" + installerForwardingConfig + "）"
	case installerSysctlBootName:
		return "IPv4 转发  安装器将把 OpenRC sysctl 服务加入开机运行级别"
	case "Mihomo 运行权限":
		if match := coreCapEffSummary.FindStringSubmatch(check.Message); len(match) == 3 {
			return "TUN 权限  Mihomo PID " + match[1] + " 持有 CAP_NET_ADMIN（CapEff=" + match[2] + "）"
		}
	case "IPv4 转发权限":
		// Passes with forwarding still off: the gateway apply turns it on later.
		if strings.Contains(check.Message, "ip_forward=1") {
			return "IPv4 转发  当前已开启，由网关应用与恢复"
		}
		return "IPv4 转发  应用网关转发时开启，当前未开启"
	case "Mihomo 服务权限":
		if strings.Contains(check.Message, "当前配置未启用 TUN") {
			return "TUN  当前配置未启用"
		}
	case "TUN 自动准备":
		return "TUN  " + firstClause(check.Message)
	}
	return ""
}

func firstClause(value string) string {
	if i := strings.IndexAny(value, "；。"); i >= 0 {
		return value[:i]
	}
	return value
}

func (r Report) print(out io.Writer, preparation bool) {
	r.printDetailed(out, preparation, true)
}

func (r Report) printDetailed(out io.Writer, preparation, heading bool) {
	if heading {
		if preparation {
			fmt.Fprintln(out, "TUN 安装准备（本机可加载驱动并设置开机加载；容器仅检查设备）")
		} else {
			fmt.Fprintln(out, "环境检查（只读，不自动修改系统）")
		}
	}
	warnings, failures := 0, 0
	for _, check := range r {
		label := "通过"
		switch check.Level {
		case "info":
			label = "信息"
		case "warn":
			label = "提醒"
			warnings++
		case "error":
			label = "失败"
			failures++
		}
		fmt.Fprintf(out, "[%s] %s：%s\n", label, printable(check.Name), printable(check.Message))
		if check.Fix != "" {
			fmt.Fprintf(out, "  处理方法：%s\n", printable(check.Fix))
		}
		for _, group := range check.Groups {
			fmt.Fprintf(out, "  %s\n", printable(group.heading()))
			for _, command := range group.Commands {
				fmt.Fprintf(out, "  命令：%s\n", printable(command))
			}
			if group.Note != "" {
				fmt.Fprintf(out, "  说明：%s\n", printable(group.Note))
			}
		}
	}
	if preparation && failures > 0 {
		fmt.Fprintf(out, "TUN 准备未完成（%d 项错误，%d 项提醒）；请按上方结果处理后重新执行。已成功加载的驱动不会卸载。\n", failures, warnings)
	} else if preparation {
		fmt.Fprintf(out, "TUN 准备完成（%d 项提醒）；继续检查核心权限、网络和端口。实际核心运行与重启后的状态仍需验证。\n", warnings)
	} else if failures > 0 {
		fmt.Fprintf(out, "环境检查未通过（%d 项错误，%d 项提醒）；请处理上述问题后重新检查。\n", failures, warnings)
	} else {
		fmt.Fprintf(out, "环境检查完成（%d 项提醒）。检测结果仅代表当前状态；流量转发与重启后的实际效果仍需验证。\n", warnings)
	}
}

func printable(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, value)
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, fmt.Errorf("无法读取 Agent 候选配置：%s", printable(path))
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("Agent 候选配置不是有效 JSON：%s", printable(path))
	}
	if cfg.ServiceType != "mihomo" && cfg.ServiceType != "mosdns" && cfg.ServiceType != "oxidns" {
		return cfg, fmt.Errorf("环境检查不支持该服务类型")
	}
	if cfg.AgentPort < 1 || cfg.AgentPort > 65535 || !filepath.IsAbs(cfg.ConfigPath) {
		return cfg, fmt.Errorf("Agent 端口或核心配置路径无效，请检查安装参数")
	}
	cfg.AgentConfigFile = path
	return cfg, nil
}

func loadCoreConfig(cfg Config) (map[string]interface{}, error) {
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("无法读取 %s 核心配置，请检查安装路径与读取权限", cfg.ServiceType)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil || len(doc) == 0 {
		// YAML errors can quote credentials or entire configuration lines.
		return nil, fmt.Errorf("%s 核心配置需要是有效、非空的 YAML 对象", cfg.ServiceType)
	}
	return doc, nil
}

func Run(configs []Config) Report {
	if len(configs) == 0 {
		return Report{{Name: "配置", Level: "error", Message: "没有待检查的 Agent 配置"}}
	}
	var report Report
	for _, cfg := range configs {
		if _, err := loadCoreConfig(cfg); err != nil {
			report = append(report, Check{Name: cfg.ServiceType + " 配置", Level: "error", Message: err.Error(), Fix: "先安装核心并确认配置路径正确，再重新生成安装脚本。"})
		}
	}
	// Continue collecting independent diagnostics even if one core is invalid.
	report = append(report, systemChecks(configs)...)
	report = append(report, portChecks(configs)...)
	return report
}
