package rulesync

import (
	"configflow/internal/deploy"
	"fmt"
	"strings"
	"testing"
)

func TestIsolationRejectsAliasAmplification(t *testing.T) {
	for _, main := range []string{"x: &x [*x]\nrules: []\n", func() string {
		var b strings.Builder
		b.WriteString("a0: &a0 [x]\n")
		for i := 1; i < 40; i++ {
			fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
		}
		b.WriteString("rules: []\n")
		return b.String()
	}()} {
		files := map[string]deploy.Content{"config.yaml": {Data: []byte(main), Role: deploy.RoleConfig}}
		m := deploy.Manifest{ProtocolVersion: 1, AgentID: "device", ProfileID: "default", ServiceType: "mihomo", DeploymentID: "old", ConfigPath: "config.yaml", Files: manifestFiles(files)}
		old, e := deploy.Pack(m, files, digest([]byte(main))[:8], nil)
		if e != nil {
			t.Fatal(e)
		}
		m.DeploymentID = "new"
		next, e := deploy.Pack(m, files, digest([]byte(main))[:8], nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = IsolateRuleBundle(old, next, "mihomo"); e == nil {
			t.Fatal("unsafe aliases accepted")
		}
	}
}
