package mosdns

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/rulefetch"
)

// Review C28 #1: hand-edited values of another JSON type (a numeric
// library_rule_id on some rule, enabled: null) no longer make the archive
// or the rule proxy refuse; they compare like Python's == and truthiness.
func TestReviewSnapshotToleratesOtherJSONTypes(t *testing.T) {
	ctx, _ := testRepo(t, nil)
	config := map[string]any{
		"profile_id":    "office",
		"system_config": map[string]any{"server_domain": "https://cf.test", "config_token": 5},
		"rule_configs": []any{
			map[string]any{"itemType": "rule", "library_rule_id": 7, "enabled": "yes"},
			map[string]any{"library_rule_id": "inline", "enabled": true},
			map[string]any{"library_rule_id": "off", "enabled": nil},
			map[string]any{"library_rule_id": "zero", "library_enabled": 0},
		},
		"rule_library": []any{
			map[string]any{"id": 7, "name": 7, "source_type": "content", "content": "x"},
			map[string]any{"id": "inline", "name": "Inline", "source_type": "content", "content": "+.a.test"},
			map[string]any{"id": "off", "name": "Off", "source_type": "content", "content": "+.off.test"},
			map[string]any{"id": "zero", "name": "Zero", "source_type": "content", "content": "+.zero.test"},
		},
	}
	rawCfg, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	nativeCfg, err := ConfigFromJSON(rawCfg)
	if err != nil {
		t.Fatal(err)
	}
	files := func(url string) error {
		_, err := BuildZIP(ctx, nativeCfg, "plugins: []", []Download{{URL: url, Name: "r", LocalPath: "r.txt"}}, nil, ArchiveOptions{})
		return err
	}
	if err := files("/api/profiles/office/rule-library/content/inline"); err != nil {
		t.Fatalf("selected inline rule: %v", err)
	}
	for _, id := range []string{"off", "zero", "7"} {
		var archive *ArchiveError
		if err := files("/api/profiles/office/rule-library/content/" + id); !errors.As(err, &archive) ||
			archive.Message != "规则“r”准备失败：规则不在当前配置快照中" {
			t.Fatalf("%s: %v", id, err)
		}
	}
	snap := snapshotOf(nativeCfg)
	if ok, err := authorized(snap, AuthRequest{Token: "5"}, nil); err != nil || ok {
		t.Fatalf("a numeric config_token never equals a query token: %v %v", ok, err)
	}
	if content, found := localContent("https://cf.test/api/profiles/office/rule-library/content/inline", snap, ""); !found || content != "+.a.test" {
		t.Fatalf("inline: %q %v", content, found)
	}
	// Python compares the netloc: user info makes the URL remote; a URL Go
	// cannot parse is remote too (the URL check then answers 400).
	for _, raw := range []string{"https://u@cf.test/api/profiles/office/rule-library/content/inline", "https://cf .test/x", "/a%zz"} {
		if _, found := localContent(raw, snap, ""); found {
			t.Fatalf("%s: local", raw)
		}
	}
}

// Review C28 #2: config.yaml with non-string keys (kept by the C21
// converter) is checked like Python; `plugins: null` and `files: null` are
// refused like Python.
func TestReviewReferenceCheck(t *testing.T) {
	files := map[string][]byte{"rules/a.txt": nil}
	for config, want := range map[string]string{
		"1: x\nplugins:\n  - {tag: a, 2: y, args: {files: [rules/a.txt]}}\n": "",
		"plugins:\n  - {tag: a, args: {files: [rules/b.txt]}}\n":             "MosDNS 配置引用了未打包的规则文件",
		"plugins:\n":                          "MosDNS 配置格式无效",
		"log: {}\n":                           "",
		"plugins:\n  - args: {files: }\n":     "MosDNS 文件引用必须为列表",
		"plugins:\n  - args: [1]\n":           "",
		"plugins:\n  - tag: a\n    args: x\n": "",
		"- a\n":                               "MosDNS 配置格式无效",
	} {
		err := checkReferences(config, files, false)
		if got := ""; err != nil {
			got = err.Error()
			if got != want {
				t.Errorf("%q: %q, want %q", config, got, want)
			}
		} else if want != "" {
			t.Errorf("%q: accepted, want %q", config, want)
		}
	}
	if err := checkReferences("plugins: [x]\n", files, false); err == nil {
		t.Fatal("a plugin that is not a mapping must fail (Python: AttributeError)")
	}
	var archive *ArchiveError
	if err := checkReferences("plugins: [x]\n", files, false); errors.As(err, &archive) {
		t.Fatal("not a user-facing archive error")
	}
}

// Review C28 #3: the rule proxy chooses 400 by Go error values of its own.
func TestReviewRuleProxyBadRequestErrors(t *testing.T) {
	for _, err := range []error{errSizeLimit, errContentLength, errNotUTF8, rulefetch.ErrInvalidURL} {
		if !isBadRequest(err) {
			t.Errorf("%v: not 400", err)
		}
	}
	if isBadRequest(&rulefetch.FetchError{Class: "ConnectionError"}) {
		t.Error("download failure is not 400")
	}
	if got := safeDetails(errNotUTF8); got != "exception_type=UnicodeDecodeError" {
		t.Errorf("safeDetails: %s", got)
	}
}
