package agents

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/deploy/pushdiff"
)

// TestReviewA13DiffRegistrationSecretAndSearch checks every diff disclosure surface.
func TestReviewA13DiffRegistrationSecretAndSearch(t *testing.T) {
	const registration = "synthetic-registration-key"
	t.Setenv("AGENT_REGISTRATION_KEY", registration)
	f := newContentHTTP(t, true, false)
	const password = "synthetic-config-password"
	main := "hosts:\n  private-" + registration + ".example.com: 192.0.2.10\nproxies:\n- name: visible-node\n  type: ss\n  server: relay.example.com\n  port: 80\n  password: " + password + "\n- name: private-node-" + registration + "-synthetic%2Dregistration%2Dkey\n  type: http\n  server: second.example.com\n  port: 80\n"
	rules := "DOMAIN,visible.example.com\nDOMAIN,中-" + registration + ".example.com\nDOMAIN,encoded-synthetic%2Dregistration%2Dkey.example.com\nDOMAIN,stored-agent-token.example.com\n"
	bundle := contentBundle(t, main, rules)
	rawFiles, err := bundle.Files()
	if err != nil {
		t.Fatal(err)
	}
	manifest := bundle.Manifest()
	files := map[string]deploy.Content{}
	for _, file := range manifest.Files {
		files[file.Path] = deploy.Content{Data: rawFiles[file.Path], Role: file.Role}
	}
	const provider = "providers/nodes.yaml"
	sum := sha256.Sum256([]byte(main))
	files[provider] = deploy.Content{Data: []byte(main), Role: deploy.RoleProvider}
	manifest.Files = append(manifest.Files, deploy.File{Path: provider, Role: deploy.RoleProvider, Size: int64(len(main)), SHA256: hex.EncodeToString(sum[:])})
	bundle, err = deploy.Pack(manifest, files, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	f.candidate.Store(bundle)
	diff := f.settle(t).WantStatus(200)
	for _, secret := range []string{registration, "synthetic%2Dregistration%2Dkey", "stored-agent-token"} {
		if diff.Contains(secret) {
			t.Errorf("push-diff leaked stored credential %q", secret)
		}
	}
	hiddenDiff, err := json.Marshal(pushdiff.WithoutSecrets(diff.Object()))
	if !diff.Contains(password) || err != nil || strings.Contains(string(hiddenDiff), password) {
		t.Error("user configuration credential reveal/hide behavior changed")
	}
	version := diff.Object().GetString("version")
	for _, path := range []string{"rules/example.list", provider} {
		pageURL := "/api/agents/content-agent/push-diff/entries?version=" + version + "&change=added&file=" + url.QueryEscape(path)
		page := f.get(pageURL).WantStatus(200)
		for _, secret := range []string{registration, "synthetic%2Dregistration%2Dkey", "stored-agent-token"} {
			if page.Contains(secret) {
				t.Errorf("entries leaked stored credential %q", secret)
			}
			search := f.get(pageURL + "&q=" + url.QueryEscape(secret)).WantStatus(200)
			if search.Object().GetInt("total") != 0 {
				t.Errorf("stored credential searchable: %q", secret)
			}
		}
		visible := "visible"
		if f.get(pageURL+"&q="+visible).WantStatus(200).Object().GetInt("total") != 1 {
			t.Error("redaction removed ordinary searchable content")
		}
		// A12's revealable user configuration credentials remain marked;
		// the dialog's hidden view must never include their bytes.
		hidden, err := json.Marshal(pushdiff.WithoutSecrets(page.Object()))
		if err != nil || strings.Contains(string(hidden), password) {
			t.Error("configuration password escaped the hidden view")
		}
	}
}
