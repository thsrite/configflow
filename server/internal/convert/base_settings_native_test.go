package convert

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"net/netip"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestNativeBaseSettingsChangedFields(t *testing.T) {
	t.Cleanup(SetMihomoIntegrations(MihomoIntegrations{
		FakeIPNetworkError: func(value string) error { _, err := netip.ParsePrefix(value); return err },
		DNSLoop:            func(*yamlx.Node, []netip.Addr) (string, string, bool, error) { return "", "", false, nil },
	}))
	tests := []struct{ name, document, previous, want string }{
		{"valid", "mixed-port: 7890\nexternal-controller: 127.0.0.1:9090", "", ""},
		{"boolean", "allow-lan: 'true'", "", "allow-lan 只能填写 true 或 false，不要加引号"},
		{"unchanged malformed", "allow-lan: 'true'\nlog-level: info", "allow-lan: 'true'\nlog-level: debug", ""},
		{"changed type", "allow-lan: 'true'", "allow-lan: true", "allow-lan 只能填写 true 或 false，不要加引号"},
		{"duplicate", "mixed-port: 9090\nexternal-controller: 127.0.0.1:9090", "", "端口 9090 同时被 mixed-port 和 external-controller 使用，请改用不同端口"},
		{"exposed secret", "external-controller: 0.0.0.0:9090", "", msgBaseSecretRequired},
		{"dns list", "dns: {nameserver: [null]}", "", "dns.nameserver 需为文本列表，每行一项，不能有空项"},
		{"integer float", "mixed-port: 7890.0", "", "mixed-port 需为 1–65535 的整数"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := yamlx.Parse([]byte(tt.document))
			if err != nil {
				t.Fatal(err)
			}
			var previous *yamlx.Node
			if tt.previous != "" {
				previous, err = yamlx.Parse([]byte(tt.previous))
				if err != nil {
					t.Fatal(err)
				}
			}
			before, _ := yamlx.Marshal(doc)
			err = ValidateBaseNode(doc, previous, nil, nil)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				var validation *BaseSettingsError
				if !errors.As(err, &validation) || err.Error() != tt.want {
					t.Fatalf("error %v want %s", err, tt.want)
				}
			}
			after, _ := yamlx.Marshal(doc)
			if string(before) != string(after) {
				t.Fatal("validator mutated its input")
			}
		})
	}
}
func TestNativeBaseSettingsAliasBudget(t *testing.T) {
	self := yamlx.Map()
	self.Anchor = "self"
	yamlx.Set(self, "self", &yamlx.Node{Kind: yaml.AliasNode, Alias: self, Value: "self"})
	if err := ValidateBaseNode(self, nil, nil, nil); !errors.Is(err, yamlx.ErrAliasRecursion) {
		t.Fatalf("recursive graph: %v", err)
	}
	leaf := yamlx.Seq(yamlx.Int(0))
	leaf.Anchor = "a0"
	for depth := 1; depth <= 40; depth++ {
		next := yamlx.Seq(leaf, &yamlx.Node{Kind: yaml.AliasNode, Alias: leaf, Value: leaf.Anchor})
		next.Anchor = fmt.Sprintf("a%d", depth)
		leaf = next
	}
	root := yamlx.Map(yamlx.Str("payload"), leaf)
	if err := ValidateBaseNode(root, nil, nil, nil); !errors.Is(err, yamlx.ErrAliasBudget) {
		t.Fatalf("depth-40 shared graph: %v", err)
	}
}
func TestNativeAgentPortProjection(t *testing.T) {
	agents := []model.Agent{{ID: "m", Name: "panel", Host: "192.0.2.1", Port: 9090}, {ID: "dns", Host: "192.0.2.1", Port: 5353, ServiceType: "oxidns"}, {ID: "remote", Host: "192.0.2.2", Port: 5354, ServiceType: "oxidns"}, {ID: "docker", Host: "192.0.2.1", Port: 9999, DeploymentMethod: "docker"}}
	ports, order, hosts := AgentPorts(agents)
	if len(ports) != 2 || len(order) != 2 || order[0] != 9090 || order[1] != 5353 || len(hosts) != 1 || hosts[0] != "192.0.2.1" {
		t.Fatal(ports, order, hosts)
	}
}
func FuzzNativeBaseSettings(f *testing.F) {
	for _, seed := range []string{"allow-lan: true", "dns: {nameserver: [https://example.com/dns-query]}", "mixed-port: 7890"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		doc, err := yamlx.Parse([]byte(text))
		if err != nil {
			return
		}
		before, err := yamlx.Marshal(doc)
		if err != nil {
			return
		}
		_ = ValidateBaseNode(doc, doc, nil, nil)
		after, err := yamlx.Marshal(doc)
		if err != nil || string(before) != string(after) {
			t.Fatal("validation changed document", err)
		}
	})
}
