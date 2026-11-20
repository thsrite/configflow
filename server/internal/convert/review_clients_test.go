package convert_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/logx"
)

// A delimiter inside an ID must not shift into the exclusion dimension of
// the remote cache key. The two Python tuple keys are distinct.
func TestLoonRemoteCacheKeepsSourceAndExclusionSeparate(t *testing.T) {
	withFakeSubStore(t)
	config := convert.JObjKV(
		"subscriptions", []any{
			convert.JObjKV("id", "a", "name", "First"),
			convert.JObjKV("id", "a\x01b", "name", "Second"),
		},
		"proxy_groups", []any{
			convert.JObjKV("id", "one", "name", "One", "type", "select", "subscriptions", []any{"a"}, "exclude_regex", "b\x01c"),
			convert.JObjKV("id", "two", "name", "Two", "type", "select", "subscriptions", []any{"a\x01b"}, "exclude_regex", "c"),
		},
	)
	out := loonOf(t, config)
	remotes := udpSection(out, "Remote Proxy")
	if len(remotes) != 2 {
		t.Fatalf("distinct sources/exclusions need two remotes, got %q", remotes)
	}
	for _, want := range []string{
		"First = /subscriptions/a/proxies?format=loon&exclude_regex=b%01c,",
		"Second = /subscriptions/a\x01b/proxies?format=loon&exclude_regex=c,",
		"One = select,First",
		"Two = select,Second",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output\n%s", want, out)
		}
	}
}

func TestSurgeDebugLogOmitsSubscriptionURLsAndTokens(t *testing.T) {
	withFakeSubStore(t)
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Debug, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		root.Close()
		if _, err := logx.Setup(logx.Config{Level: logx.Critical}); err != nil {
			t.Error(err)
		}
	})
	config := convert.JObjKV(
		"system_config", convert.JObjKV("server_domain", "https://panel.example.com", "config_token", "synthetic-config-token"),
		"subscriptions", []any{convert.JObjKV("id", "feed", "name", "Feed")},
		"proxy_groups", []any{convert.JObjKV("id", "group", "name", "Proxy", "type", "select", "subscriptions", []any{"feed"})},
	)
	surgeOf(t, config)
	for _, secret := range []string{"synthetic-config-token", "https://panel.example.com", "/subscriptions/"} {
		if strings.Contains(logs.String(), secret) {
			t.Errorf("debug log contains %q", secret)
		}
	}
	if !strings.Contains(logs.String(), "Generated Surge proxy group") {
		t.Fatal("group generation was not exercised with debug logging")
	}
}

// Expected results come from the Python public config/subscription entry
// points. Cases target source branches absent from the production snapshot.
func TestClientReviewBoundaryReferences(t *testing.T) {
	withFakeSubStore(t)
	rows := loadJSONLines(t, "testdata/review_clients.jsonl.gz")
	const indexPath = "testdata/n88c_review_classifications.json.gz"
	index := readClientClassifications(t, "review_clients.jsonl.gz", indexPath)
	var classifications []clientClassification
	used := map[int]bool{}
	for i, entry := range rows {
		t.Run(entry.GetString("case")+"/"+entry.GetString("fn"), func(t *testing.T) {
			issue := clientEntryIssue(t, entry)
			classification := clientClassification{Row: i, Fn: entry.GetString("fn"), InputSHA: generationDigest([]byte(encode(entry.Value("args")))), Field: issue, Family: clientIssueFamily(issue)}
			if issue != "" {
				classifications = append(classifications, classification)
			}
			if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") != "1" {
				expected, found := index[i]
				if found {
					used[i] = true
				}
				if (issue != "") != found || (found && expected != classification) {
					t.Fatalf("review helper classification changed: %s", issue)
				}
			}
			msg := runSurgeLoonRow(t, rowOf(entry))
			if issue == "" && msg != "" {
				t.Fatal(msg)
			}
		})
	}
	if os.Getenv("CONFIGFLOW_RECORD_CLIENT_INDEX") != "1" && len(used) != len(index) {
		t.Fatal("unused review classifications")
	}
	if !t.Failed() {
		writeClientClassifications(t, "review_clients.jsonl.gz", indexPath, classifications)
	}
	t.Logf("%d Python-derived config/subscription boundaries; classified %d", len(rows), len(classifications))
}
