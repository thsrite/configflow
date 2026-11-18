package dnsha

import (
	"testing"

	"configflow/internal/deploy"
)

func FuzzHAListener(f *testing.F) {
	for _, seed := range []string{config, "plugins: []\n", "plugins: &p [*p]\n", "plugins: [{type: udp_server, args: {listen: '[::]:53'}}]\n", "plugins: [{type: udp_server, args: {listen: '192.0.2.20:53'}}]\n", "not: [valid"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		if len(content) > 1<<20 {
			t.Skip()
		}
		m := deploy.Manifest{ProtocolVersion: 1, AgentID: "primary", ProfileID: "default", DeploymentID: "fuzz-candidate", ServiceType: "oxidns", ConfigPath: "config.yaml", Files: []deploy.File{{Path: "config.yaml", Role: deploy.RoleConfig, Size: int64(len(content)), SHA256: digest([]byte(content))}}}
		b, e := deploy.Pack(m, map[string]deploy.Content{"config.yaml": {Data: []byte(content), Role: deploy.RoleConfig}}, "version", nil)
		if e != nil {
			t.Fatal(e)
		}
		_ = requireListener(b)
	})
}
