package routes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func diagnosticFixture(t *testing.T, content string) (string, *DeploymentManifest) {
	t.Helper()
	stage := t.TempDir()
	if err := os.WriteFile(filepath.Join(stage, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return stage, &DeploymentManifest{ConfigPath: "config.yaml", Files: []DeploymentFile{{Path: "config.yaml", Role: "config"}}}
}

func requireDiagnostic(t *testing.T, err error, values ...string) string {
	t.Helper()
	if err == nil {
		t.Fatal("expected validation failure")
	}
	_, details := deploymentErrorFields(err)
	for _, value := range values {
		if !strings.Contains(details, value) {
			t.Fatalf("missing %q in diagnostics: %s", value, details)
		}
	}
	return details
}

func TestMosDNSPluginFailureLocations(t *testing.T) {
	for _, test := range []struct{ name, content, field, line, snippet string }{
		{"duplicate", "plugins:\n  - tag: duplicate\n    type: sequence\n  - tag: duplicate\n    type: sequence\n", "plugins[1].tag", "4", "tag: duplicate"},
		{"missing-type", "plugins:\n  - tag: incomplete\n    args: []\n", "plugins[0]", "2", "tag: incomplete"},
		{"missing-file", "plugins:\n  - tag: grok\n    type: domain_set\n    args:\n      files:\n        - rules/grok.txt\n", "plugins[0].args.files[0]", "6", "rules/grok.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stage, manifest := diagnosticFixture(t, test.content)
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(test.content), &doc); err != nil {
				t.Fatal(err)
			}
			err := validateMosDNS(doc, stage, manifest)
			requireDiagnostic(t, err, "文件：config.yaml（生成配置）", "位置："+test.field, "行号："+test.line, test.snippet)
		})
	}
}

func TestOxiDNSPluginFailureLocations(t *testing.T) {
	for _, content := range []string{
		"plugins:\n  - tag: grok\n    type: domain_set\n    args:\n      files:\n        - rules/grok.txt\n",
		"plugins:\n  - tag: grok\n    type: domain_set\n    args:\n      persistent:\n        files:\n          - rules/grok.txt\n",
	} {
		stage, manifest := diagnosticFixture(t, content)
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		cfg := &Config{ServiceType: "oxidns", ConfigPath: filepath.Join(stage, "live.yaml")}
		err := validateOxiDNS(cfg, doc, stage, filepath.Join(stage, "config.yaml"), manifest)
		requireDiagnostic(t, err, "来源：插件 grok", "plugins[0].args.", "files[0]", "内容：- rules/grok.txt")
	}
}

func TestNativeResourceErrorIncludesRulesetAndOriginalLine(t *testing.T) {
	stage, manifest := diagnosticFixture(t, "plugins: []\n")
	manifest.Files = append(manifest.Files, DeploymentFile{Path: "rules/grok.txt", Name: "grok 用户规则", Role: "rule"})
	if err := os.MkdirAll(filepath.Join(stage, "rules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "rules/grok.txt"), []byte("# comment\nfull:example.com\n11\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cause := errors.New("exit status 1")
	err := diagnosticFailure("OxiDNS native configuration check failed", cause, []byte("ERROR loading rules/grok.txt: line 3: invalid domain rule"))
	err = nativeConfigurationLocation(err, stage, filepath.Join(stage, "config.yaml"), manifest, false)
	requireDiagnostic(t, err, "文件：rules/grok.txt", "来源：grok 用户规则", "行号：3", "内容：11", "invalid domain rule")
	if !errors.Is(err, cause) {
		t.Fatal("location discarded cause")
	}
}

func TestNativeResourceErrorDoesNotGuessConfigLine(t *testing.T) {
	stage, manifest := diagnosticFixture(t, "mode: rule\nrules:\n  - MATCH,DIRECT\n")
	for _, output := range []string{"ERROR nested resource yaml: line 3: invalid value", "ERROR rules[99] invalid", "ERROR proxy 42: invalid", "ERROR provider parse failed: proxy 0: unsupported type"} {
		err := nativeConfigurationLocation(diagnosticFailure("check failed", errors.New("exit 1"), []byte(output)), stage, filepath.Join(stage, "config.yaml"), manifest, true)
		details := requireDiagnostic(t, err, "文件：config.yaml", output)
		if strings.Contains(details, "行号：") || strings.Contains(details, "内容：") {
			t.Fatalf("guessed line: %s", details)
		}
	}
}

func TestNativeResourceMentionsMustMatchFailingFile(t *testing.T) {
	stage, manifest := diagnosticFixture(t, "mode: rule\n")
	manifest.Files = append(manifest.Files, DeploymentFile{Path: "grok.txt", Name: "grok"})
	for _, output := range []string{"INFO loading grok.txt\nERROR unrelated plugin", "ERROR loading other-grok.txt: line 2: invalid"} {
		err := nativeConfigurationLocation(diagnosticFailure("check failed", nil, []byte(output)), stage, filepath.Join(stage, "config.yaml"), manifest, true)
		details := requireDiagnostic(t, err, "文件：config.yaml")
		if strings.Contains(details, "来源：grok") {
			t.Fatalf("guessed file: %s", details)
		}
	}
}

func TestNativeResourceLocationMatchesCompletePaths(t *testing.T) {
	stage, manifest := diagnosticFixture(t, "mode: rule\nrules:\n  - MATCH,DIRECT\n")
	for _, resource := range []struct{ path, content string }{
		{"rules/grok.txt", "WRONG-SUFFIX-FILE"},
		{"custom/rules/grok.txt", "ACTUAL-ERROR-LINE"},
		{"rules/error.txt", "INFORMATIONAL-RESOURCE"},
	} {
		manifest.Files = append(manifest.Files, DeploymentFile{Path: resource.path, Name: resource.path})
		name := filepath.Join(stage, resource.path)
		if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("# header\n"+resource.content+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{"custom/rules/grok.txt", "./custom/rules/grok.txt", filepath.Join(stage, "custom/rules/grok.txt")} {
		for _, output := range []string{fmt.Sprintf("ERROR loading %q: line 2: invalid", path), path + ":2:5: error: invalid resource"} {
			err := nativeConfigurationLocation(diagnosticFailure("check failed", nil, []byte(output)), stage, filepath.Join(stage, "config.yaml"), manifest, false)
			details := requireDiagnostic(t, err, "文件：custom/rules/grok.txt", "行号：2", "内容：ACTUAL-ERROR-LINE")
			if strings.Contains(details, "WRONG-SUFFIX-FILE") {
				t.Fatal("a resource suffix was mistaken for the full path")
			}
		}
	}
	for _, output := range []string{
		"ERROR loading /unmanaged/custom/rules/grok.txt: line 2: invalid",
		"ERROR loading missing/rules/grok.txt: line 2: invalid",
		"/unmanaged/custom/rules/grok.txt:2:5: error: invalid resource",
		"missing/rules/grok.txt:2:5: error: invalid resource",
		"INFO loading rules/error.txt\nERROR unrelated plugin failed",
		"time=now level=info msg=\"loaded rules/error.txt\"\nERROR unrelated plugin failed",
	} {
		err := nativeConfigurationLocation(diagnosticFailure("check failed", nil, []byte(output)), stage, filepath.Join(stage, "config.yaml"), manifest, false)
		details := requireDiagnostic(t, err, "文件：config.yaml")
		if strings.Contains(details, "行号：") || strings.Contains(details, "内容：") || strings.Contains(details, "来源：") {
			t.Fatalf("a missing or informational resource was given a false location: %s", details)
		}
	}
}

func TestNativeExactConfigFileColumnLocation(t *testing.T) {
	stage, manifest := diagnosticFixture(t, "mode: rule\nrules:\n  - MATCH,DIRECT\n")
	config := filepath.Join(stage, "config.yaml")
	for _, path := range []string{"config.yaml", "./config.yaml", config} {
		err := nativeConfigurationLocation(diagnosticFailure("check failed", nil, []byte(path+":3:5: error: invalid rule")), stage, config, manifest, false)
		requireDiagnostic(t, err, "文件：config.yaml（生成配置）", "行号：3\n", "内容：- MATCH,DIRECT")
	}
	for _, output := range []string{
		"/unmanaged/config.yaml:3:5: error: invalid rule",
		"nested/config.yaml:3:5: error: invalid rule",
		"time=now level=info msg=\"config.yaml:3:5: check completed\"\nERROR unrelated failure",
	} {
		err := nativeConfigurationLocation(diagnosticFailure("check failed", nil, []byte(output)), stage, config, manifest, false)
		details := requireDiagnostic(t, err, "文件：config.yaml")
		if strings.Contains(details, "行号：") || strings.Contains(details, "内容：") {
			t.Fatalf("unrelated location mapped to the generated config: %s", details)
		}
	}
}

func TestLocationSnippetsDoNotExposeCredentials(t *testing.T) {
	for _, test := range []struct {
		content string
		line    int
		secret  string
	}{
		{"proxies:\n - name: opaque-private-value\n   password: opaque-private-value\n", 2, "opaque-private-value"},
		{"private-key: |\n  opaque-private-line\nbroken: [\n", 2, "opaque-private-line"},
		{"uuid: opaque-uuid\nbroken: [\n", 1, "opaque-uuid"},
		{"url: https://example.test/sub?token=secret-url-value\n", 1, "secret-url-value"},
		{"-----BEGIN PRIVATE KEY-----\nPRIVATE-KEY-LINE\n-----END PRIVATE KEY-----\n", 2, "PRIVATE-KEY-LINE"},
		{"-----BEGIN OPENSSH PRIVATE KEY-----\nOPENSSH-KEY-LINE\n-----END OPENSSH PRIVATE KEY-----\n", 2, "OPENSSH-KEY-LINE"},
		{"proxies: [{name: Broken, password: \"hidden-first\n    hidden-second\" broken}]\n", 2, "hidden-second"},
		{"proxies: [{name: Broken, password: \"hidden first\n    hidden-second\" broken}]\n", 1, "hidden first"},
		{"proxies: [{name: Broken, password: 'hidden ''quote\n    hidden-second' broken}]\n", 2, "hidden-second"},
		{"proxies: [{name: Broken, password: \"hidden \\\"quote\n    hidden-second\" broken}]\n", 2, "hidden-second"},
	} {
		err := diagnosticLocation(errors.New("invalid field"), "config.yaml", "", "", test.line, []byte(test.content), true)
		summary, details := deploymentErrorFields(err)
		if strings.Contains(summary+details, test.secret) {
			t.Fatalf("credential leaked: %s", details)
		}
		if !strings.Contains(details, "REDACTED") {
			t.Fatalf("missing safe excerpt: %s", details)
		}
	}
}

func TestSecretExcerptProtectionEndsAfterTheSecret(t *testing.T) {
	for _, content := range []string{
		"-----BEGIN PRIVATE KEY-----\nprivate-line\n-----END PRIVATE KEY-----\nvisible: diagnostic\n",
		"password: \"private-first\n private-second\"\nvisible: diagnostic\n",
		"password: 'private-first\n private-second'\nvisible: diagnostic\n",
	} {
		line := len(strings.Split(strings.TrimSuffix(content, "\n"), "\n"))
		if got := sourceLine([]byte(content), line); got != "visible: diagnostic" {
			t.Fatalf("non-sensitive diagnostic hidden after credential: %q", got)
		}
	}
}

func TestLocationDetailsAreBoundedAndKeepFinalCoreError(t *testing.T) {
	output := []byte(strings.Repeat("INFO intermediate output\n", 3000) + "ERROR 最后一条错误\n")
	err := diagnosticLocation(diagnosticFailure("check failed", nil, output), "config.yaml", "用户规则", "rules[0]", 1, []byte(strings.Repeat("测", 3000)), true)
	err = deploymentStageLocation(err, &Config{ConfigPath: "/srv/config.yaml"}, "verifying")
	_, details := deploymentErrorFields(err)
	if len(details) > maxDeploymentErrorDetails || !utf8.ValidString(details) {
		t.Fatal("unbounded diagnostic")
	}
	requireDiagnostic(t, err, "文件：config.yaml", "来源：用户规则", "行号：1", "最后一条错误")
}

func TestDeploymentStageLocationKeepsFileOperationAndCause(t *testing.T) {
	pathErr := &os.PathError{Op: "open", Path: "/tmp/rules/grok.txt", Err: os.ErrPermission}
	err := deploymentStageLocation(pathErr, &Config{ConfigPath: "/srv/config.yaml"}, "replacing")
	requireDiagnostic(t, err, "发布阶段：replacing", "目标配置：/srv/config.yaml", "文件：/tmp/rules/grok.txt", "文件操作：open", "permission denied")
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal("cause lost")
	}
}

func TestYAMLSyntaxAndProviderReferenceFailuresRetainLocations(t *testing.T) {
	for _, test := range []struct{ content, want string }{
		{"mode: rule\nrules: [\ninvalid: true\n", "行号："},
		{"rule-providers:\n  grok:\n    type: file\n    path: missing.txt\n", "位置：rule-providers.grok.path"},
	} {
		cfg := testDeploymentConfig(t)
		if err := os.WriteFile(cfg.ConfigPath, []byte("old: configuration\n"), 0600); err != nil {
			t.Fatal(err)
		}
		state := submitBundle(t, cfg, "location-failure", map[string][]byte{"config.yaml": []byte(test.content)}, true)
		if state.Status != "failed" || !strings.Contains(state.ErrorDetails, test.want) || !strings.Contains(state.ErrorDetails, "发布阶段：verifying") {
			t.Fatalf("missing failure context: %+v", state)
		}
		live, _ := os.ReadFile(cfg.ConfigPath)
		if string(live) != "old: configuration\n" {
			t.Fatalf("validation changed live config: %q", live)
		}
	}
}

func TestDiagnosticFileReadIsBounded(t *testing.T) {
	name := filepath.Join(t.TempDir(), "large.txt")
	if err := os.WriteFile(name, []byte(strings.Repeat("x", 3<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	if size := len(readDiagnosticFile(name)); size != 2<<20 {
		t.Fatalf("unexpected diagnostic read size %d", size)
	}
}
