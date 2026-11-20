package oxidns

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/yamlx"
)

// TestReviewC29ReaderCausesReachConsumers prevents a reader refusal from
// becoming the ordinary syntax fallback or losing its policy/alias diagnostic.
func TestReviewC29ReaderCausesReachConsumers(t *testing.T) {
	var amplified strings.Builder
	amplified.WriteString("- &a0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&amplified, "- &a%d [*a%d, *a%d]\n", i, i-1, i-1)
	}
	for _, test := range []struct {
		name, source string
		cause        error
	}{
		{"duplicate", "- {addr: first.example, addr: second.example}", yamlx.ErrDuplicateKey},
		{"tag-shape", "- !!seq scalar", yamlx.ErrPolicy},
		{"merge", "- {<<: 5}", yamlx.ErrPolicy},
		{"recursive", "- &a [*a]", yamlx.ErrAliasRecursion},
		{"budget", amplified.String(), yamlx.ErrAliasBudget},
	} {
		for _, field := range []string{"custom_config", "local_dns", "remote_dns", "fallback_dns"} {
			t.Run(test.name+"/"+field, func(t *testing.T) {
				settings := M("local_dns", "192.0.2.53", "remote_dns", "192.0.2.54")
				set(settings, field, S(test.source))
				profile, err := NewTypedProfile(nil)
				if err != nil {
					t.Fatal(err)
				}
				set(profile.doc, "oxidns", settings)
				if output, err := Generate(profile, "http://config.example"); output != "" || !errors.Is(err, test.cause) {
					t.Errorf("Generate lost reader cause: %q %v", output, err)
				}
				if field != "custom_config" {
					empty, err := NewTypedProfile(nil)
					if err != nil {
						t.Fatal(err)
					}
					draft, err := PrepareEditor(M("local_dns", "192.0.2.53", "remote_dns", "192.0.2.54", "fallback_dns", "192.0.2.55"), empty, "")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := ApplyCommonSettings(draft, M(field, test.source), empty, ""); !errors.Is(err, test.cause) {
						t.Errorf("ApplyCommonSettings lost reader cause: %v", err)
					}
				}
			})
		}
	}
}

// TestReviewC29ArchiveLimits uses the shared materializer's limits through the
// OxiDNS owner, including canonical paths and both attachment/download budgets.
func TestReviewC29ArchiveLimits(t *testing.T) {
	var requests atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected source fetch", http.StatusBadGateway)
	}))
	defer upstream.Close()
	beforeFiles, beforeBytes := mosdns.MaxFiles, mosdns.MaxTotalBytes
	t.Cleanup(func() { mosdns.MaxFiles, mosdns.MaxTotalBytes = beforeFiles, beforeBytes })
	mosdns.MaxFiles, mosdns.MaxTotalBytes = 3, 64
	for _, test := range []struct {
		name      string
		files     []File
		downloads []mosdns.Download
		message   string
	}{
		{"count", []File{{Path: "a"}, {Path: "b"}, {Path: "c"}}, nil, "生成配置超过打包大小限制"},
		{"bytes", []File{{Path: "large", Content: []byte(strings.Repeat("x", 64))}}, nil, "生成配置超过打包大小限制"},
		{"duplicate canonical paths", []File{{Path: "./a", Content: []byte("first")}, {Path: "a", Content: []byte("second")}}, nil, "生成配置包含内容冲突的文件路径"},
		{"configuration", []File{{Path: "./config.yaml", Content: []byte("plugins: []")}}, nil, "生成配置包含内容冲突的文件路径"},
		{"download count", nil, []mosdns.Download{
			{LocalPath: "a", URL: upstream.URL + "/api/rule-library/content/inline"},
			{LocalPath: "b", URL: upstream.URL + "/api/rule-library/content/inline"},
			{LocalPath: "c", URL: upstream.URL + "/api/rule-library/content/inline"},
		}, "生成配置超过打包文件数量限制"},
	} {
		t.Run(test.name, func(t *testing.T) {
			sourceType, content := "content", "example.com"
			config := &model.Config{
				ProfileID:    "default",
				SystemConfig: model.SystemConfig{ServerDomain: upstream.URL},
				RuleLibrary:  []model.RuleLibraryItem{{ID: "inline", SourceType: &sourceType, Content: &content}},
				Profile:      &model.Profile{RuleConfigs: []model.RuleConfig{{LibraryRuleID: "inline"}}},
			}
			output, err := BuildTypedZIP(context.Background(), config, "plugins: []", test.downloads, test.files, upstream.URL)
			var failure *mosdns.ArchiveError
			if output != nil || !errors.As(err, &failure) || failure.Message != test.message {
				t.Fatalf("wrong archive refusal: %d bytes, %v, want %q", len(output), err, test.message)
			}
		})
	}
	empty := &model.Config{Profile: &model.Profile{}}
	for _, path := range []string{"../escape", "a/../../escape", "/absolute", "a//b", "a/./b", "a\\..\\escape", "a\x00b", "a\nb", "././state/learned.txt"} {
		t.Run(path, func(t *testing.T) {
			for _, download := range []bool{false, true} {
				var files []File
				var downloads []mosdns.Download
				if download {
					downloads = []mosdns.Download{{LocalPath: path, URL: upstream.URL + "/never-fetched"}}
				} else {
					files = []File{{Path: path}}
				}
				if output, err := BuildTypedZIP(context.Background(), empty, "plugins: []", downloads, files, ""); err == nil || output != nil {
					t.Fatalf("unsafe path escaped: %q %v", path, err)
				}
			}
		})
	}
	if requests.Load() != 0 {
		t.Fatal("archive rejection performed source I/O")
	}
	files := []File{{Path: "a", Content: []byte(strings.Repeat("x", 26))}, {Path: "b", Content: []byte(strings.Repeat("x", 27))}}
	if output, err := BuildTypedZIP(context.Background(), empty, "plugins: []", nil, files, ""); err != nil || len(output) == 0 {
		t.Fatalf("exact file/byte limits rejected: %v", err)
	}
}
