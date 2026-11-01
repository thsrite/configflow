package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFinalPreviewAndInstalledConfigurationAreIdentical(t *testing.T) {
	cfg := testDeploymentConfig(t)
	input := "mode: rule\nproxy-providers:\n  feed:\n    type: file\n    path: ./providers/feed.yaml\nrules: [MATCH,DIRECT]\n"
	body, _ := json.Marshal(map[string]string{"content": input})
	w := httptest.NewRecorder()
	FinalConfigPreviewHandler(cfg)(w, httptest.NewRequest(http.MethodPost, "/api/config/final-preview", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body)
	}
	var preview struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
		t.Fatal("preview must not write live configuration")
	}
	files := map[string][]byte{"config.yaml": []byte(preview.Content), "providers/feed.yaml": []byte("proxies: []\n")}
	s := submitBundle(t, cfg, "final-preview", files, true, func(m *DeploymentManifest) { m.FinalConfig = true })
	if s.Status != "succeeded" {
		t.Fatalf("publication: %s %s %s", s.Status, s.Error, s.ErrorDetails)
	}
	installed, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(installed) != preview.Content {
		t.Fatalf("installed configuration differs: %v\n%s\n%s", err, installed, preview.Content)
	}
}

func TestFinalConfigurationRejectsUnpreviewedRewrites(t *testing.T) {
	cfg := testDeploymentConfig(t)
	old := []byte("mode: direct\n")
	if err := os.WriteFile(cfg.ConfigPath, old, 0600); err != nil {
		t.Fatal(err)
	}
	// A hand-crafted noncanonical file may not be silently rewritten under the
	// final-config contract, including mere path/serialization changes.
	s := submitBundle(t, cfg, "unpreviewed", map[string][]byte{"config.yaml": []byte("mode: rule\nrules: [MATCH,DIRECT]\n")}, false,
		func(m *DeploymentManifest) { m.FinalConfig = true })
	if s.Status != "failed" || !strings.Contains(s.Error, "预览不一致") {
		t.Fatalf("rewritten final configuration accepted: %+v", s)
	}
	if actual, _ := os.ReadFile(cfg.ConfigPath); !bytes.Equal(actual, old) {
		t.Fatal("live config changed")
	}
}

func TestFinalPreviewIncludesTargetDNSAdaptation(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.0.2.15", nil })
	cfg := &Config{ServiceType: "mihomo", ConfigPath: filepath.Join(t.TempDir(), "main.yaml"), NativeAIO: true, AgentIP: "192.0.2.15"}
	content, err := renderFinalConfig(cfg, "tun: {enable: true, auto-redirect: true}\n")
	if err != nil || !strings.Contains(content, "192.0.2.15/32") {
		t.Fatalf("missing device adaptation: %v %s", err, content)
	}
	second, err := renderFinalConfig(cfg, content)
	if err != nil || second != content {
		t.Fatalf("final render is not stable: %v", err)
	}
}
