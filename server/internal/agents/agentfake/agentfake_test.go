package agentfake_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
)

func TestUploadContract(t *testing.T) {
	data := []byte("rules: []\n")
	h := sha256.Sum256(data)
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "a11-example", AgentID: "agent-example", ProfileID: "profile-example", ServiceType: "mihomo", ConfigPath: "config.yaml", Files: []deploy.File{{Path: "config.yaml", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:]), Role: deploy.RoleConfig}}}
	b, err := deploy.Pack(m, map[string]deploy.Content{"config.yaml": {Data: data, Role: deploy.RoleConfig}}, "version", nil)
	if err != nil {
		t.Fatal(err)
	}
	archive, _ := b.Archive()
	sha, _ := b.SHA256()
	a := agentfake.New(agentfake.Options{AgentID: m.AgentID, Token: "test-token"})
	srv := httptest.NewServer(a)
	defer srv.Close()
	for _, tc := range []struct {
		name, id, sum string
		status        int
	}{{"upload", m.DeploymentID, sha, 202}, {"replay", m.DeploymentID, sha, 200}, {"invalid-id", "_bad", sha, 400}, {"digest", m.DeploymentID, "bad", 400}} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest("POST", srv.URL+"/api/deployments", bytes.NewReader(archive))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer test-token")
			req.Header.Set("X-Deployment-ID", tc.id)
			req.Header.Set("X-Content-SHA256", tc.sum)
			res, err := srv.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			var result json.RawMessage
			_ = json.NewDecoder(res.Body).Decode(&result)
			if res.StatusCode != tc.status {
				t.Fatalf("%d %s", res.StatusCode, result)
			}
		})
	}
	calls := a.Calls()
	if len(calls) != 4 || calls[0].Manifest == nil || calls[0].FileDigests["config.yaml"] != hex.EncodeToString(h[:]) {
		t.Fatalf("transcript %#v", calls)
	}
}
