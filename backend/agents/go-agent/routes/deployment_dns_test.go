package routes

import (
	"agent/preflight"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func useDNSCheck(t *testing.T, check func(preflight.DNSCandidate, preflight.DNSOptions) preflight.Report) {
	t.Helper()
	previous := runDeploymentDNSChecks
	runDeploymentDNSChecks = check
	t.Cleanup(func() { runDeploymentDNSChecks = previous })
}

func TestDeploymentDNSBlocksBeforeServiceOrLiveFileChanges(t *testing.T) {
	cfg := testDeploymentConfig(t)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	marker := filepath.Join(filepath.Dir(cfg.ConfigPath), "running")
	os.WriteFile(marker, nil, 0600)
	useDNSCheck(t, func(candidate preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
		if candidate.Config.ConfigPath != cfg.ConfigPath || !strings.Contains(string(candidate.Content), "mode: rule") {
			t.Error("DNS validation must use staged content but live service identity")
		}
		return preflight.Report{{Name: "DNS UDP/53", Level: "error", Message: "监听端口被其他服务占用", Fix: "调整 DNS 监听端口"}}
	})
	s := submitBundle(t, cfg, "dns-block", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, true)
	if s.Status != "failed" || s.FailedStage != "verifying" || len(s.DNSChecks) != 1 || s.DNSCheckedAt == "" {
		t.Fatalf("DNS failure report lost: %+v", s)
	}
	data, _ := os.ReadFile(cfg.ConfigPath)
	if string(data) != "mode: old\n" {
		t.Fatal("DNS rejection changed live file")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("DNS rejection stopped service")
	}
	if s.BackupComplete {
		t.Fatal("DNS rejection occurred after backup boundary")
	}
}

func TestDeploymentDNSWarningsDoNotBlockAndAreReturned(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.Token = "private-agent-token"
	useDNSCheck(t, func(_ preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
		return preflight.Report{{Name: "DNS 查询", Level: "warn", Message: "当前查询超时 private-agent-token core-private-secret", Fix: "检查当前 DNS；发布后再次验证"}}
	})
	s := submitBundle(t, cfg, "dns-warn", map[string][]byte{"config.yaml": []byte("mode: rule\nsecret: core-private-secret\n")}, true)
	if s.Status != "succeeded" || len(s.DNSChecks) != 1 || s.DNSChecks[0].Level != "warn" {
		t.Fatalf("warning blocked publication or disappeared: %+v", s)
	}
	w := httptest.NewRecorder()
	DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodGet, "/api/deployments/dns-warn", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"dns_checks"`) || !strings.Contains(body, `"dns_checked_at"`) {
		t.Fatalf("missing public report: %s", body)
	}
	if strings.Contains(body, "private-agent-token") || strings.Contains(body, "core-private-secret") {
		t.Fatal("DNS report exposed credentials")
	}
}

func TestDeploymentDNSRechecksReadyCandidateBeforeActivation(t *testing.T) {
	cfg := testDeploymentConfig(t)
	os.WriteFile(cfg.ConfigPath, []byte("mode: old\n"), 0600)
	calls := 0
	useDNSCheck(t, func(_ preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
		calls++
		if calls == 1 {
			return preflight.Report{{Name: "DNS", Level: "pass", Message: "当前无冲突"}}
		}
		return preflight.Report{{Name: "DNS", Level: "error", Message: "准备完成后，端口被其他程序占用"}}
	})
	s := submitBundle(t, cfg, "dns-ready", map[string][]byte{"config.yaml": []byte("mode: rule\n")}, false)
	if s.Status != "ready" {
		t.Fatalf("prepare failed: %+v", s)
	}
	w := httptest.NewRecorder()
	DeploymentHandler(cfg)(w, httptest.NewRequest(http.MethodPost, "/api/deployments/dns-ready/activate", nil))
	if w.Code != 202 {
		t.Fatalf("activation: %d %s", w.Code, w.Body.String())
	}
	s = waitDeploymentState(t, cfg, "dns-ready", true, func(st *DeploymentState) bool {
		return st.Status == "failed" || st.Status == "succeeded"
	}, "activation dns-ready")
	if s == nil || s.Status != "failed" || len(s.DNSChecks) != 1 || s.DNSChecks[0].Level != "error" {
		t.Fatalf("stale ready check reused: %+v", s)
	}
	data, _ := os.ReadFile(cfg.ConfigPath)
	if string(data) != "mode: old\n" {
		t.Fatal("activation conflict changed live config")
	}
}

func TestDeploymentDNSUsesOriginalAIOPorts(t *testing.T) {
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	cfg.AgentIP = "192.168.0.33"
	content := "dns: {enable: true, listen: '127.0.0.2:5353'}\ntun: {enable: true, dns-hijack: ['any:5353', 'tcp://any:5353']}\n"
	useDNSCheck(t, func(candidate preflight.DNSCandidate, _ preflight.DNSOptions) preflight.Report {
		assertOriginalDNSDocument(t, cfg, candidate.Content, []byte(content))
		if !candidate.Config.NativeAIO {
			t.Error("DNS check must retain AIO identity")
		}
		return preflight.Report{{Name: "DNS", Level: "pass", Message: "已检查用户配置"}}
	})
	s := submitBundle(t, cfg, "dns-effective", map[string][]byte{"config.yaml": []byte(content)}, false)
	if s.Status != "ready" {
		t.Fatalf("prepare failed: %+v", s)
	}
}

func TestDeploymentDNSPeerUsesRegisteredCustomPath(t *testing.T) {
	cfg := &Config{ServiceType: "mihomo", NativeAIO: true}
	metadata, _ := json.Marshal(preflight.Config{ServiceType: "oxidns", NativeAIO: true, ConfigPath: "/custom/oxidns/main.yaml"})
	files := map[string][]byte{
		"/opt/configflow-agent/oxidns/config.json": metadata,
		"/custom/oxidns/main.yaml":                 []byte("plugins: []\n"),
	}
	read := func(name string) ([]byte, error) {
		if value, ok := files[name]; ok {
			return value, nil
		}
		return nil, fmt.Errorf("not found")
	}
	peer, report := deploymentDNSPeer(cfg, read)
	if peer == nil || peer.Config.ConfigPath != "/custom/oxidns/main.yaml" || len(report) != 0 {
		t.Fatalf("custom path not respected: %+v %+v", peer, report)
	}
	delete(files, "/custom/oxidns/main.yaml")
	peer, report = deploymentDNSPeer(cfg, read)
	if peer != nil || len(report) != 1 || report[0].Level != "warn" {
		t.Fatal("missing peer must be unverified, not fail or pass")
	}
}

func TestDeploymentDNSPeerOnlyInExplicitAIO(t *testing.T) {
	t.Setenv("ENABLE_OXIDNS", "false")
	t.Setenv("ENABLE_MOSDNS", "false")
	t.Setenv("ENABLE_MIHOMO", "false")
	peer, report := deploymentDNSPeer(&Config{ServiceType: "mihomo"}, func(string) ([]byte, error) {
		t.Fatal("single-service deployment read an unrelated DNS service")
		return nil, nil
	})
	if peer != nil || len(report) != 0 {
		t.Fatal("single-service deployment treated as AIO")
	}
	t.Setenv("ENABLE_OXIDNS", "true")
	t.Setenv("ENABLE_MIHOMO", "true")
	if dnsPeerService(&Config{ServiceType: "mihomo"}) != "oxidns" || dnsPeerService(&Config{ServiceType: "oxidns"}) != "mihomo" {
		t.Fatal("Docker AIO peer detection failed")
	}
}
