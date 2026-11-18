package convert

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestClientTypedProtocolScalars(t *testing.T) {
	for _, tt := range []struct {
		source, want string
		valid        bool
	}{
		{`"123"`, "123", true}, {`123`, "123", true}, {`0`, "0", true}, {`1.2500`, "1.2500", true}, {`100000000000000000000000000000001`, "100000000000000000000000000000001", true}, {`1e400`, "1e400", true},
		{`true`, "", false}, {`[]`, "", false}, {`{}`, "", false},
	} {
		t.Run(tt.source, func(t *testing.T) {
			var value clientText
			err := json.Unmarshal([]byte(tt.source), &value)
			if (err == nil) != tt.valid || string(value) != tt.want {
				t.Fatal(value, err)
			}
		})
	}
}
func TestClientTypedOptionsR5AndNull(t *testing.T) {
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close(); _, _ = logx.Setup(logx.Config{Level: logx.Critical}) })
	node, err := yamlx.FromJSON([]byte(`{"password":123,"cipher":null,"udp":"false","tls":1,"alpn":["h2","http/1.1"],"ws-opts":{"path":"/x","headers":{"Host":"example.com"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	before, err := yamlx.ToJSON(node)
	if err != nil {
		t.Fatal(err)
	}
	options, err := decodeClientOptions(node)
	if err != nil {
		t.Fatal(err)
	}
	if clientOption(options.Password, "") != "123" || options.Cipher != nil || clientFlag(options.UDP) || clientFlag(options.TLS) {
		t.Fatal("scalar/R5 projection", options)
	}
	if !model.IssueUnder(model.Issues(options), "udp") || !model.IssueUnder(model.Issues(options), "tls") {
		t.Fatal("missing invalid-bool metadata")
	}
	if strings.Join(options.ALPN.strings(), ",") != "h2,http/1.1" || options.WS == nil || options.WS.Headers.Host.values[0] != "example.com" {
		t.Fatal("nested options")
	}
	if !strings.Contains(logs.String(), fmt.Sprintf(msgClientBoolean, "udp")) || !strings.Contains(logs.String(), fmt.Sprintf(msgClientBoolean, "tls")) {
		t.Fatal("invalid booleans were not warned", logs.String())
	}
	after, _ := yamlx.ToJSON(node)
	if !bytes.Equal(before, after) {
		t.Fatal("decode rewrote input")
	}
}
func TestClientSectionsAndURL(t *testing.T) {
	sections := splitClientSections("[General]\na = 1\n[Script]\nx\n[General]\nb = 2")
	if len(sections) != 2 || sections[0].name != "General" || sections[0].text != "[General]\nb = 2" || sections[1].text != "[Script]\nx" {
		t.Fatal(sections)
	}
	config := &model.Config{ProfileID: "p", SystemConfig: model.SystemConfig{ServerDomain: " https://example.com "}}
	if clientBaseURL(config, "fallback") != "https://example.com" || clientAPIPath(config, "loon") != "/api/config/p/loon" {
		t.Fatal("URL projection")
	}
}

func TestClientTypedSurgeALPNWarnings(t *testing.T) {
	var logs bytes.Buffer
	root, err := logx.Setup(logx.Config{Level: logx.Warning, Console: &logs})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close(); _, _ = logx.Setup(logx.Config{Level: logx.Critical}) })
	if value := surgeALPN(clientOptions{ALPNRaw: json.RawMessage(`[]`)}, "Synthetic node"); value != "" || logs.Len() != 0 {
		t.Fatal("empty ALPN list should remain unconfigured", value, logs.String())
	}
	for _, raw := range []string{`["h2","http/1.1"]`, `["h3",1]`, `[null]`} {
		options := clientOptions{ALPNRaw: json.RawMessage(raw)}
		if value := surgeALPN(options, "Synthetic node"); value != "" {
			t.Fatal(value)
		}
		if !strings.Contains(logs.String(), fmt.Sprintf(msgSurgeALPN, "Synthetic node", raw)) {
			t.Fatal("missing named/original ALPN warning", logs.String())
		}
	}
}
