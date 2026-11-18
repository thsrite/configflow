package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func TestMihomoNumericPasswordPreservesTag(t *testing.T) {
	for _, source := range []string{
		`{"name":"N","type":"ss","server":"example.com","port":443,"params":{"cipher":"aes-128-gcm","password":123}}`,
		`{"name":"N","proxy_string":"type: ss\nserver: example.com\nport: 443\ncipher: aes-128-gcm\npassword: 123\n"}`,
	} {
		t.Run(source, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(source), &node); err != nil {
				t.Fatal(err)
			}
			proxy, err := NodeProxy(context.Background(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			password := yamlx.Get(proxy, "password")
			if password == nil || password.ShortTag() != "!!int" || password.Value != "123" {
				t.Fatalf("password lost or tag changed: %+v", password)
			}
			output, err := yamlx.Marshal(proxy)
			if err != nil || !bytes.Contains(output, []byte("password: 123\n")) {
				t.Fatalf("Mihomo output lost password: %s %v", output, err)
			}
		})
	}
}

func TestMihomoNumericServer(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"N","type":"http","server":123,"port":80}`), &node); err != nil {
		t.Fatal(err)
	}
	proxy, err := NodeProxy(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	server := yamlx.Get(proxy, "server")
	if server == nil || server.ShortTag() != "!!str" || server.Value != "123" {
		t.Fatalf("numeric server lost: %+v", server)
	}
}

func TestMihomoQuotedPort(t *testing.T) {
	for _, literal := range []string{`"443"`, `"00443"`} {
		t.Run(literal, func(t *testing.T) {
			var node model.Node
			if err := json.Unmarshal([]byte(`{"name":"N","type":"http","server":"example.com","port":`+literal+`}`), &node); err != nil || node.Port != 443 || model.IssueUnder(model.Issues(node), "port") {
				t.Fatalf("port lost during typed decode: %d %v", node.Port, err)
			}
			before, err := model.Marshal(node)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := NodeProxy(t.Context(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			port := yamlx.Get(proxy, "port")
			if port == nil || port.Value != "443" || port.ShortTag() != "!!int" {
				t.Fatalf("quoted Mihomo port was not normalized: %+v", port)
			}
			after, err := model.Marshal(node)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("generation changed stored port: %s %v", after, err)
			}
		})
	}
}

// Remaining archived helper rows use these test-only adapters. Runtime code
// no longer models CPython set layout or Python's bool/integer key equality.
func sortPorts(values []jsonx.Number) {
	sort.Slice(values, func(i, j int) bool { a, _ := values[i].BigInt(); b, _ := values[j].BigInt(); return a.Cmp(b) < 0 })
}

// NodeReplayIssue identifies malformed members whose dynamic behavior is not
// an oracle for the typed node converter. The predicate only reads types.
func NodeReplayIssue(node *jsonx.Object) string {
	if node == nil {
		return "node object"
	}
	for _, key := range []string{"id", "name", "type", "server"} {
		if value, present := node.Get(key); present {
			if _, ok := value.(string); !ok {
				return "node." + key + " string"
			}
		}
	}
	if value, present := node.Get("proxy_string"); present && value != nil {
		if _, ok := value.(string); !ok {
			return "node.proxy_string string"
		}
	}
	if value, present := node.Get("params"); present {
		if object, ok := value.(*jsonx.Object); !ok || object == nil {
			return "node.params object"
		}
	}
	if value, present := node.Get("port"); present && value == nil {
		return "node.port integer"
	}
	if _, text := node.Value("port").(string); text {
		return "node.port quoted integer native boundary"
	}
	data, err := json.Marshal(node)
	if err != nil {
		return "node nonfinite JSON"
	}
	var typed model.Node
	if err := json.Unmarshal(data, &typed); err != nil {
		return "node type"
	}
	if model.IssueUnder(model.Issues(typed), "port") {
		return "node.port integer"
	}
	if text, ok := node.Value("proxy_string").(string); ok {
		return structuredTypeIssue(text)
	}
	return ""
}

// NodeProfileReplayIssue applies the migrated node schema to generation rows.
func NodeProfileReplayIssue(config *jsonx.Object) string {
	if config == nil {
		return "config object"
	}
	if raw, present := config.Get("nodes"); present {
		nodes, ok := raw.([]any)
		if !ok {
			return "nodes array"
		}
		for _, raw := range nodes {
			node, ok := raw.(*jsonx.Object)
			if !ok || node == nil {
				return "node object"
			}
			if issue := NodeReplayIssue(node); issue != "" {
				return issue
			}
		}
	}
	if raw, present := config.Get("system_config"); present {
		system, ok := raw.(*jsonx.Object)
		if !ok || system == nil {
			return "system_config object"
		}
		if raw, present := system.Get("github_proxy_domain"); present {
			if _, ok := raw.(string); !ok {
				return "github_proxy_domain string"
			}
		}
	}
	return ""
}

func structuredTypeIssue(text string) string {
	if strings.HasPrefix(strings.TrimSpace(text), "{") {
		// The old JSON reader accepted Python's NaN/Infinity extensions. They
		// are not JSON; a YAML flow document reads those words as strings.
		if value, err := jsonx.ParseString(text); err == nil && !json.Valid([]byte(text)) {
			if _, ok := value.(*jsonx.Object); ok {
				return "structured nonfinite JSON"
			}
		}
	}
	var node *yamlx.Node
	var err error
	if json.Valid([]byte(text)) {
		node, err = yamlx.FromJSON([]byte(text))
	} else {
		node, err = yamlx.Parse([]byte(text))
	}
	if err != nil {
		return ""
	} // existing syntax/policy oracle still applies
	typeNode := yamlx.Resolve(yamlx.Get(node, "type"))
	if typeNode == nil || typeNode.ShortTag() == "!!null" {
		return ""
	}
	if typeNode.Kind != yaml.ScalarNode {
		return "structured type string"
	}
	data, err := yamlx.ToJSON(typeNode)
	if err != nil {
		return ""
	}
	var value string
	if json.Unmarshal(data, &value) != nil {
		return "structured type string"
	}
	return ""
}

func portReplayIssue(document *jsonx.Object) string {
	check := func(value any) string {
		if value == nil {
			return ""
		}
		number, ok := value.(jsonx.Number)
		if !ok {
			return "port integer"
		}
		if !number.IsInt() {
			return "port integer notation"
		}
		if _, err := strconv.ParseInt(string(number), 10, 64); err != nil {
			return "port int64 range"
		}
		return ""
	}
	for _, key := range InboundPortKeys {
		if issue := check(document.Value(key)); issue != "" {
			return issue
		}
	}
	for _, item := range document.GetList("listeners") {
		if listener, ok := item.(*jsonx.Object); ok {
			if issue := check(listener.Value("port")); issue != "" {
				return issue
			}
		}
	}
	if controller, ok := document.Value("external-controller").(string); ok && controller != "" {
		_, port, err := net.SplitHostPort(controller)
		if err != nil {
			return "controller host:port"
		}
		if _, err := strconv.ParseUint(port, 10, 63); err != nil {
			return "controller port"
		}
	}
	return ""
}

func nodeRowIssue(row corpusRow) string {
	switch row.fn {
	case "convert_node_to_mihomo":
		return NodeReplayIssue(obj(row.args[0]))
	case "raw_dialer":
		return NodeReplayIssue(obj(row.args[0]))
	case "dialer_target", "resolve_dialer", "overlay_dialer":
		if issue := NodeReplayIssue(obj(row.args[1])); issue != "" {
			return issue
		}
		return NodeProfileReplayIssue(obj(row.args[0]))
	case "validate_dialers", "dependency_graph", "incoming_dialers", "dependency_node_ids", "reject_surge_dialers", "validate_shapes", "group_dependencies":
		return NodeProfileReplayIssue(obj(row.args[0]))
	case "parse_structured_proxy_string":
		return structuredTypeIssue(str(row.args[0]))
	case "apply_github_proxy_domain":
		if config := obj(row.args[1]); config != nil {
			if value, present := config.Get("system_config"); present {
				if _, ok := value.(*jsonx.Object); !ok {
					return "system_config object"
				}
			}
		}
	case "mihomo_used_ports", "mihomo_listen_ports", "listen_ports_ordered", "used_ports_repr":
		return portReplayIssue(obj(row.args[0]))
	}
	return ""
}

func nativePortReplay(t *testing.T, row corpusRow, result any, err error) bool {
	t.Helper()
	if row.fn != "listen_ports_ordered" && row.fn != "used_ports_repr" {
		return false
	}
	if err != nil {
		t.Fatalf("normal port document: %v", err)
	}
	got, ok := result.([]any)
	if !ok {
		t.Fatal("port collection is not a list")
	}
	want, ok := row.out.([]any)
	if !ok {
		t.Fatal("port oracle is not a list")
	}
	// CPython set slot order is explicitly outside parity (PORTING). Compare
	// every port and owner, retaining duplicates, without a runtime simulator.
	if compactText(t, sortedSet(got)) != compactText(t, sortedSet(want)) {
		t.Errorf("port owners differ: got %s want %s", compactText(t, got), compactText(t, want))
	}
	return true
}

func TestNativeNodeR5AndParamsValidation(t *testing.T) {
	for _, tt := range []struct{ field, raw, want string }{
		{"name", "5", `{"name":"5","type":"http","server":"example.com","port":80}`},
		{"server", "false", `{"name":"outer","type":"http","server":"","port":80}`},
		{"port", `"80"`, `{"name":"outer","type":"http","server":"example.com","port":80}`},
		{"proxy_string", "[]", `{"name":"outer","type":"http","server":"example.com","port":80}`},
		{"params", "null", `{"name":"outer","type":"http","server":"example.com","port":80}`},
	} {
		t.Run(tt.field, func(t *testing.T) {
			value := mustJSON(t, `{"name":"outer","type":"http","server":"example.com","port":80}`)
			raw, err := jsonx.ParseString(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			value.Set(tt.field, raw)
			node, err := decodeLegacy[model.Node](value)
			if err != nil {
				t.Fatal(err)
			}
			proxy, err := NodeProxy(context.Background(), node, nil)
			if err != nil {
				t.Fatal(err)
			}
			data, err := yamlx.ToJSON(proxy)
			if err != nil || string(data) != tt.want {
				t.Fatalf("got %s/%v want %s", data, err, tt.want)
			}
		})
	}
	for _, raw := range []string{`[]`, `5`, `false`, `"bad"`} {
		var node model.Node
		if err := json.Unmarshal([]byte(`{"type":"http","params":`+raw+`}`), &node); err != nil {
			t.Fatal(err)
		}
		if _, err := NodeProxy(context.Background(), node, nil); err == nil || err.Error() != msgNodeParamsObject {
			t.Fatalf("params %s: %v", raw, err)
		}
	}
	for _, value := range []string{"false", "0", "1", "[]", "[http]", "{x: y}", "!!float 1"} {
		if proxy, err := StructuredProxy("type: " + value); proxy != nil || err == nil || err.Error() != msgProxyYAMLInvalid+": "+msgProxyTypeString {
			t.Fatalf("type %s: %v %v", value, proxy, err)
		}
	}
}

func TestNativeNodeR2AndOverlayOrder(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"name":"outer","type":"HTTP","server":"example.com","port":80.0,"params":{"name":"inner","password":"0o17"}}`), &node); err != nil {
		t.Fatal(err)
	}
	proxy, err := NodeProxy(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := yamlx.ToJSON(proxy)
	if err != nil || string(data) != `{"name":"inner","type":"http","server":"example.com","port":80.0,"password":"0o17"}` {
		t.Fatalf("%s %v", data, err)
	}
}

func TestNativeNodeFixPreservesInputParams(t *testing.T) {
	const source = `{"name":"node","type":"vless","port":443,"params":{"reality-opts": { "public-key" : "example" }}}`
	var node model.Node
	if err := json.Unmarshal([]byte(source), &node); err != nil {
		t.Fatal(err)
	}
	before, err := model.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := NodeProxy(context.Background(), node, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := model.Marshal(node)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("input record bytes changed")
	}
	original, err := node.Params.Node()
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Get(yamlx.Get(original, "reality-opts"), "short-id") != nil {
		t.Fatal("input received a default")
	}
	shortID := yamlx.Get(yamlx.Get(proxy, "reality-opts"), "short-id")
	if shortID == nil || shortID.ShortTag() != "!!str" || shortID.Value != "" {
		t.Fatal("output is missing its short-id default")
	}
}

func TestNativeStructuredNonfiniteJSONUsesYAMLStrings(t *testing.T) {
	for _, word := range []string{"NaN", "Infinity", "-Infinity"} {
		proxy, err := StructuredProxy(`{"type":"http","port":` + word + `}`)
		if err != nil {
			t.Fatal(err)
		}
		port := yamlx.Get(proxy, "port")
		if port == nil || port.ShortTag() != "!!str" || port.Value != word {
			t.Fatalf("%s: %#v", word, port)
		}
	}
}

func TestNativeIntegerPortOwners(t *testing.T) {
	document, err := yamlx.Parse([]byte("port: 7890\nsocks-port: 7890\nlisteners: [{name: a, port: 8082}, {name: b, port: 8081}]\nexternal-controller: ':9090'"))
	if err != nil {
		t.Fatal(err)
	}
	owners := ListenPorts(document)
	want := []ListeningPort{{7890, "socks-port"}, {8081, "listeners"}, {8082, "listeners"}, {9090, "external-controller"}}
	if fmt.Sprint(owners) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", owners, want)
	}
	for _, value := range []string{"true", "false", "[]", "{}", "100000000000000000000", "7890.0"} {
		node, err := yamlx.Parse([]byte("port: " + value))
		if err != nil {
			t.Fatal(err)
		}
		if len(UsedPorts(node)) != 0 {
			t.Fatalf("%s counted as integer port", value)
		}
	}
}

func TestNativeNodeAliasBoundaries(t *testing.T) {
	var source strings.Builder
	source.WriteString("type: http\na0: &a0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&source, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, tt := range []struct {
		text string
		want error
	}{{source.String(), yamlx.ErrAliasBudget}, {"type: http\na: &a [*a]", yamlx.ErrAliasRecursion}} {
		if node, err := StructuredProxy(tt.text); node != nil || !errors.Is(err, tt.want) {
			t.Fatalf("structured graph: %v %v", node, err)
		}
		text := tt.text
		if node, err := NodeProxy(context.Background(), model.Node{Name: "example", ProxyString: &text}, nil); node != nil || !errors.Is(err, tt.want) {
			t.Fatalf("node graph: %v %v", node, err)
		}
	}
}

func FuzzTypedNodeProxy(f *testing.F) {
	for _, source := range []string{
		`{"name":"node","type":"HTTP","server":"example.com","port":80.0,"params":{"password":"0o17"}}`,
		`{"name":"node","type":"http","params":{"value":1e400}}`,
		`{"name":"node","type":"vless","params":{"reality-opts":{"public-key":"example"}}}`,
		`{"name":"node","proxy_string":"type: http\na: &a [*a]"}`,
		`{"name":"node","proxy_string":"{\"type\":\"http\",\"name\":\"\\ud83c\\udded\"}"}`,
	} {
		f.Add(source)
	}
	f.Fuzz(func(t *testing.T, source string) {
		var node model.Node
		if json.Unmarshal([]byte(source), &node) != nil {
			return
		}
		before, err := model.Marshal(node)
		if err != nil {
			t.Fatal(err)
		}
		proxy, convertErr := NodeProxy(context.Background(), node, nil)
		after, err := model.Marshal(node)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("conversion changed the stored record")
		}
		if convertErr != nil || proxy == nil {
			return
		}
		if scalarText(yamlx.Get(proxy, "type")) == "vless" {
			reality := yamlx.Resolve(yamlx.Get(proxy, "reality-opts"))
			if reality != nil && reality.Kind == yaml.MappingNode && yamlx.Get(reality, "short-id") == nil {
				t.Fatal("VLESS output is missing short-id")
			}
		}
		data, err := yamlx.Marshal(proxy)
		if err != nil {
			if !errors.Is(err, yamlx.ErrPolicy) || len(data) != 0 {
				t.Fatalf("unexpected writer failure: %v", err)
			}
			return
		}
		parsed, err := yamlx.Parse(data)
		if err != nil || !yamlx.Equal(proxy, parsed) {
			t.Fatalf("proxy YAML changed value: %v", err)
		}
	})
}

func usedPorts(value *jsonx.Object) ([]any, error) {
	ports, err := MihomoUsedPorts(value)
	if err != nil {
		return nil, err
	}
	keys := make([]jsonx.Number, 0, len(ports))
	for key := range ports {
		keys = append(keys, key)
	}
	sortPorts(keys)
	result := make([]any, len(keys))
	for i, key := range keys {
		result[i] = key
	}
	return result, nil
}
