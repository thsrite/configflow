package preflight

import (
	"agent/clioutput"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompactReportRetainsRuntimeFactsAndCompleteEvidence(t *testing.T) {
	report := Report{
		{Name: "TUN 设备", Level: "pass", Message: "设备可以打开并读取 TUNGETFEATURES；未创建接口。"},
		{Name: "TUN 实际启动", Level: "pass", Message: "Mihomo 核心 PID 35445 正持有内核 TUN/TAP 接口 Meta，已确认接口存在于该进程的网络命名空间；不代表路由验证。"},
		{Name: "IPv4 转发", Level: "pass", Message: "当前 net.ipv4.ip_forward=1；不代表重启后开启。"},
		{Name: "IPv4 转发持久化", Level: "warn", Message: "未找到持久化设置。", Fix: "检查 /etc/sysctl.d/。"},
	}
	var terminal, details bytes.Buffer
	report.PrintWithOptions(&terminal, clioutput.Options{Details: &details}, false)
	for _, value := range []string{"Meta", "35445", "3 项通过，1 项提醒", "未找到持久化设置", "检查 /etc/sysctl.d/"} {
		if !strings.Contains(terminal.String(), value) {
			t.Fatalf("missing key result %q: %s", value, terminal.String())
		}
	}
	if strings.Contains(terminal.String(), "TUNGETFEATURES") || !strings.Contains(details.String(), "TUNGETFEATURES") || !strings.Contains(details.String(), "网络命名空间") {
		t.Fatalf("incorrect evidence separation: terminal=%q log=%q", terminal.String(), details.String())
	}
	var verbose bytes.Buffer
	report.PrintWithOptions(&verbose, clioutput.Options{Verbose: true}, false)
	if !strings.Contains(verbose.String(), "TUNGETFEATURES") || !strings.Contains(verbose.String(), "处理方法") {
		t.Fatalf("verbose lost full detail: %s", verbose.String())
	}
}

func TestReportShowsAllFailuresAndRepairsWithoutTerminalInjection(t *testing.T) {
	report := Report{
		{Name: "TUN", Level: "error", Message: "设备不可用", Fix: "在 PVE 添加设备"},
		{Name: "IPv4", Level: "warn", Message: "转发关闭", Fix: "按网关用途开启"},
		{Name: "配置\n[通过]fake", Level: "pass", Message: "path\x1b[2J"},
	}
	var output bytes.Buffer
	report.PrintWithOptions(&output, clioutput.Options{Verbose: true}, false)
	text := output.String()
	if !report.HasErrors() || !strings.Contains(text, "1 项错误，1 项提醒") || !strings.Contains(text, "在 PVE 添加设备") || !strings.Contains(text, "按网关用途开启") {
		t.Fatalf("missing aggregated diagnostics: %s", text)
	}
	if strings.Contains(text, "\x1b") || strings.Contains(text, "\n[通过]fake") {
		t.Fatalf("unescaped terminal controls: %q", text)
	}
	if (Report{{Level: "warn"}}).HasErrors() {
		t.Fatal("advisory warning must not fail a DNS-only installation")
	}
}

func TestCandidateMetadataDoesNotRetainCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "candidate.json")
	data := `{"service_type":"mihomo","agent_port":8080,"config_path":"/etc/mihomo/config.yaml","native_aio":true,"agent_service_unit":"configflow-agent-mihomo","token":"PRIVATE-TOKEN","server_url":"https://PRIVATE-URL"}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil || !cfg.NativeAIO || cfg.AgentConfigFile != path || cfg.AgentServiceUnit != "configflow-agent-mihomo" {
		t.Fatalf("lost metadata: %+v %v", cfg, err)
	}
	serialized, err := json.Marshal(cfg)
	if err != nil || strings.Contains(string(serialized), "PRIVATE") {
		t.Fatalf("preflight retained registration credentials: %v", err)
	}
}

func TestCoreParsingErrorsDoNotPrintConfigValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("secret: [PRIVATE-TOKEN\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := loadCoreConfig(Config{ServiceType: "mihomo", ConfigPath: path})
	if err == nil || strings.Contains(err.Error(), "PRIVATE") {
		t.Fatalf("unsafe YAML error: %v", err)
	}
}
