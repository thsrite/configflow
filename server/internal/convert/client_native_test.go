package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestTypedClientNativeBoundaries(t *testing.T) {
	tests := []struct{ options, contains, absent string }{
		{`"reserved":[1,2,3]`, `client-id = 1/2/3`, `client-id = [`},
		{`"reserved":"AQID"`, `client-id = AQID`, ""},
		{`"client-id":"3/2/1","reserved":[1,2,3]`, `client-id = 3/2/1`, `client-id = 1/2/3`},
	}
	for _, tt := range tests {
		t.Run(tt.options, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(`{"name":"WG","type":"wg","server":"example.com","port":51820,"params":{"public-key":"public",`+tt.options+`}}`), &node); err != nil {
				t.Fatal(err)
			}
			_, section, err := SurgeNode(node)
			if err != nil || !strings.Contains(section, tt.contains) || (tt.absent != "" && strings.Contains(section, tt.absent)) {
				t.Fatal(section, err)
			}
		})
	}
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"H","type":"hysteria2","server":"example.com","port":443,"params":{"down":"٣٠mbps"}}`), &node); err != nil {
		t.Fatal(err)
	}
	line, err := LoonNode(node)
	if err != nil || strings.Contains(line, "download-bandwidth=") {
		t.Fatal(line, err)
	}
}
func TestTypedClientGroupNullHealthDefaults(t *testing.T) {
	for _, text := range []string{`{"id":"g","name":"G","type":"url-test","url":null,"interval":null,"manual_nodes":["DIRECT"]}`, `{"id":"g","name":"G","type":"select","follow_group":"source"}`} {
		var group model.ProxyGroup
		if err := json.Unmarshal([]byte(text), &group); err != nil {
			t.Fatal(err)
		}
		source := model.ProxyGroup{ID: "source", Type: "url-test", ManualNodes: &[]string{"DIRECT"}}
		config := &model.Config{Profile: &model.Profile{ProxyGroups: []model.ProxyGroup{group, source}}}
		line, err := SurgeGroup(group, config, "", nil)
		if err != nil || line != "G = url-test, DIRECT, url = "+defaultTestURL+", interval = 300" {
			t.Fatal(line, err)
		}
	}
}
func TestTypedClientAliasPolicy(t *testing.T) {
	recursion := "a: &a {b: *a}"
	expanded := "a0: &a0 [x]\n"
	for i := 1; i <= 40; i++ {
		expanded += fmtAliasLevel(i)
	}
	for _, text := range []string{recursion, expanded} {
		node := model.Node{Name: "N", ProxyString: &text}
		_, _, surgeErr := SurgeNode(node)
		_, loonErr := LoonNode(node)
		for _, err := range []error{surgeErr, loonErr} {
			if !errors.Is(err, yamlx.ErrAliasRecursion) && !errors.Is(err, yamlx.ErrAliasBudget) {
				t.Fatalf("unsafe YAML not rejected: %v", err)
			}
		}
	}
}
func fmtAliasLevel(index int) string {
	return "a" + strconv.Itoa(index) + ": &a" + strconv.Itoa(index) + " [*a" + strconv.Itoa(index-1) + ", *a" + strconv.Itoa(index-1) + "]\n"
}
func FuzzTypedClientNodes(f *testing.F) {
	for _, seed := range []string{`{"type":"wg","params":{"mtu":"1e1000000000","keepalive":"1e-1000000000"}}`, `{"name":"N","type":"ss","server":"example.com","port":443,"params":{"password":123}}`, `{"type":"wg","params":{"reserved":[1,2,3],"udp":"false"}}`, `{"type":"vless","params":{"reality-opts":{}}}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		var node model.Node
		if json.Unmarshal([]byte(text), &node) != nil || node.ProxyString != nil {
			return
		}
		before, err := model.Marshal(node)
		if err != nil {
			return
		}
		_, _, _ = SurgeNode(node)
		_, _ = LoonNode(node)
		after, err := model.Marshal(node)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("client renderer changed stored input", err)
		}
	})
}
func FuzzTypedClientGeneration(f *testing.F) {
	env := MihomoEnvironment{ValidateRules: func(*model.Config, []string, string) error { return nil }}
	env.Clients = ClientCollaborators{ParseNodes: func(string) ([]ClientSource, error) { return nil, nil }, FilterNodes: func(nodes []model.Node, _ *string, _ *string) ([]model.Node, error) { return nodes, nil }}
	f.Cleanup(SetMihomoEnvironment(env))
	f.Add(`{"proxy_groups":[{"id":"g","name":"G","type":"select","manual_nodes":["DIRECT"]}],"nodes":[]}`)
	f.Add(`{"proxy_groups":[{"id":"g","name":"G","type":"url-test","url":null,"interval":null,"manual_nodes":["n"]}],"nodes":[{"id":"n","name":"N","type":"http","server":"example.com","port":80}]}`)
	f.Fuzz(func(t *testing.T, text string) {
		var profile model.Profile
		var shared model.Shared
		if json.Unmarshal([]byte(text), &profile) != nil || json.Unmarshal([]byte(text), &shared) != nil {
			return
		}
		config := &model.Config{Profile: &profile, Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}
		before, err := model.Marshal(config)
		if err != nil {
			return
		}
		for _, generate := range []func(context.Context, *model.Config, string) (string, error){SurgeConfig, LoonConfig} {
			first, firstErr := generate(context.Background(), config, "")
			second, secondErr := generate(context.Background(), config, "")
			if first != second || (firstErr == nil) != (secondErr == nil) {
				t.Fatal("unstable generation")
			}
		}
		after, err := model.Marshal(config)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("generation changed stored input", err)
		}
	})
}

func TestTypedClientALPN(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`"h3"`, ", alpn=h3"}, {`["h3"]`, ", alpn=h3"}, {`["h2","http/1.1"]`, ""}, {`["h3",1]`, ""}, {`[1]`, ""}, {`[null]`, ""}, {`[]`, ""},
	} {
		t.Run(tt.input, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(`{"name":"N","type":"tuic","server":"example.com","port":443,"params":{"alpn":`+tt.input+`}}`), &node); err != nil {
				t.Fatal(err)
			}
			line, _, err := SurgeNode(node)
			if err != nil {
				t.Fatal(err)
			}
			prefix := "N = tuic-v5, example.com, 443"
			if line != prefix+tt.want {
				t.Fatal(line)
			}
		})
	}
}

func TestTypedClientR5Families(t *testing.T) {
	tests := []struct {
		name, source, want string
		reject             bool
	}{
		{"fractional port", `{"name":"N","type":"http","server":"example.com","port":1.5}`, "N = http,example.com,0", false},
		{"out of range port", `{"name":"N","type":"http","server":"example.com","port":100000000000000000000}`, "N = http,example.com,0", false},
		{"wrong params", `{"name":"N","type":"http","server":"example.com","port":80,"params":[]}`, "", true},
		{"null text", `{"name":"N","type":"ss","server":"example.com","port":443,"params":{"password":null}}`, `N = Shadowsocks,example.com,443,aes-256-gcm,"",fast-open=false,udp=false`, false},
		{"zero text", `{"name":"N","type":"anytls","server":"example.com","port":443,"params":{"sni":0}}`, `N = AnyTLS,example.com,443,"",sni=0,skip-cert-verify=false,udp=false`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(tt.source), &node); err != nil {
				t.Fatal(err)
			}
			line, err := LoonNode(node)
			if (err != nil) != tt.reject || line != tt.want {
				t.Fatal(line, err)
			}
		})
	}
	for _, proxy := range []*yamlx.Node{nil, yamlx.Str("bad"), yamlx.Seq(yamlx.Str("bad"))} {
		for _, target := range []string{"surge", "loon"} {
			out, err := ClientProxyText([]*yamlx.Node{proxy}, func([]*yamlx.Node) ([]model.Node, error) {
				t.Fatal("invalid proxy reached owner callback")
				return nil, nil
			}, target)
			if out != "" || err == nil || err.Error() != msgClientProxyObject {
				t.Fatal(out, err)
			}
		}
	}
}

func TestTypedClientWrongTextRejected(t *testing.T) {
	for _, field := range []string{"name", "type", "server"} {
		t.Run(field, func(t *testing.T) {
			source := `{"id":"n","name":"N","type":"http","server":"example.com","port":80}`
			source = strings.Replace(source, `"`+field+`":"`+map[string]string{"name": "N", "type": "http", "server": "example.com"}[field]+`"`, `"`+field+`":[]`, 1)
			var node model.Node
			if err := json.Unmarshal([]byte(source), &node); err != nil {
				t.Fatal(err)
			}
			line, section, err := SurgeNode(node)
			want := fmt.Sprintf(msgMihomoStoredText, kindNode, "n", field, "[]")
			if line != "" || section != "" || err == nil || err.Error() != want {
				t.Fatal(line, section, err)
			}
			line, err = LoonNode(node)
			if line != "" || err == nil || err.Error() != want {
				t.Fatal(line, err)
			}
		})
	}
}

func TestTypedClientQuantityKeepsExtremeNonzero(t *testing.T) {
	for _, text := range []string{"1e1000000000", "1e-1000000000", "1e999999999999999999999999", "1e-400"} {
		value := clientText(text)
		if !clientQuantity(&value) {
			t.Fatal("nonzero quantity lost", text)
		}
	}
	for _, text := range []string{"0", "0.0", "-0", "0e1000000000"} {
		value := clientText(text)
		if clientQuantity(&value) {
			t.Fatal("zero quantity emitted", text)
		}
	}
}

// Shared model coercion is an explicit external dependency, not a native
// exception. These assertions become active as soon as the model repair lands.
func TestTypedClientSharedIntegerStrings(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"N","type":"http","server":"example.com","port":"443"}`), &node); err != nil {
		t.Fatal(err)
	}
	if model.IssueUnder(model.Issues(node), "port") {
		t.Fatal("shared model integer-string entry did not decode port")
	}
	line, _, err := SurgeNode(node)
	if err != nil || line != "N = http, example.com, 443" {
		t.Fatal(line, err)
	}
	var group model.ProxyGroup
	if err = json.Unmarshal([]byte(`{"id":"g","name":"G","type":"url-test","interval":"300"}`), &group); err != nil {
		t.Fatal(err)
	}
	if model.IssueUnder(model.Issues(group), "interval") {
		t.Fatal("shared integer coercion did not cover group interval")
	}
	line, err = LoonGroup(group, []string{"DIRECT"})
	if err != nil || line != "G = url-test,DIRECT,url="+LoonDefaultTestURL+",interval=300,tolerance=100" {
		t.Fatal(line, err)
	}
}
func TestTypedClientSharedNumericText(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"id":"n","name":123,"type":"http","server":456,"port":443}`), &node); err != nil {
		t.Fatal(err)
	}
	if model.IssueUnder(model.Issues(node), "name") || model.IssueUnder(model.Issues(node), "server") {
		t.Fatal("shared model numeric-string entry did not decode name/server")
	}
	line, _, err := SurgeNode(node)
	if err != nil || line != "123 = http, 456, 443" {
		t.Fatal(line, err)
	}
}

func TestTypedClientJSONKeepsNativeDuplicateKeys(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{Clients: defaultClientCollaborators()}))
	for _, source := range []string{
		`{"name":"ignored","type":"ss","server":"example.com","port":443,"password":"first","password":"last"}`,
	} {
		t.Run(source, func(t *testing.T) {
			node := model.Node{Name: "Outer", ProxyString: &source}
			line, _, err := SurgeNode(node)
			if err != nil || line != "Outer = ss, example.com, 443, encrypt-method=aes-256-gcm, password=last" {
				t.Fatal(line, err)
			}
			line, err = LoonNode(node)
			if err != nil || line != `Outer = Shadowsocks,example.com,443,aes-256-gcm,"last",fast-open=false,udp=false` {
				t.Fatal(line, err)
			}
		})
	}
}

func TestTypedClientChecksOnlyConsumedText(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{Clients: defaultClientCollaborators()}))
	var node model.Node
	if err := json.Unmarshal([]byte(`{"id":"n","name":"Outer","type":false,"server":[],"params":[],"proxy_string":"{\"name\":false,\"type\":\"http\",\"server\":\"example.com\",\"port\":80}"}`), &node); err != nil {
		t.Fatal(err)
	}
	// The source supplies type/server/params, and the outer record supplies name.
	// Wrong-typed unused fields must not reject that effective, valid node.
	line, _, err := SurgeNode(node)
	if err != nil || line != "Outer = http, example.com, 80" {
		t.Fatal(line, err)
	}
	line, err = LoonNode(node)
	if err != nil || line != "Outer = http,example.com,80" {
		t.Fatal(line, err)
	}
}

func TestTypedClientParsedTextErrorKeepsOwnerID(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{Clients: defaultClientCollaborators()}))
	source := `{"type":"http","server":[],"port":80}`
	node := model.Node{ID: "owner", Name: "Outer", ProxyString: &source}
	line, _, err := SurgeNode(node)
	if line != "" || err == nil || err.Error() != fmt.Sprintf(msgMihomoStoredText, kindNode, "owner", "server", "[]") {
		t.Fatal(line, err)
	}
}

func TestTypedClientSurgeBuiltinsAreNotManualNodes(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(MihomoEnvironment{ValidateRules: func(*model.Config, []string, string) error { return nil }}))
	for _, id := range []string{"DIRECT", "REJECT"} {
		t.Run(id, func(t *testing.T) {
			config := &model.Config{Profile: &model.Profile{ProxyGroups: []model.ProxyGroup{{ID: "g", Name: "G", Type: "select", ManualNodes: &[]string{id}, ProxiesOrder: &[]model.ResourceRef{{Type: "node", ID: id}}}}}, Nodes: []model.Node{{ID: id, Name: "N", Type: "http", Server: "example.com", Port: 80}}}
			out, err := SurgeConfig(context.Background(), config, "")
			if err != nil || strings.Contains(out, "N = http") || !strings.Contains(out, "G = select, "+id) {
				t.Fatal(out, err)
			}
		})
	}
}
