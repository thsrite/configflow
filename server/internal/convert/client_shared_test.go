package convert

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func TestTypedClientSharedScalarOrList(t *testing.T) {
	for _, source := range []string{
		`{"alpn":"h3","dns":"example.com","ws-opts":{"path":"/ws","headers":{"Host":"example.com"}}}`,
		`{"alpn":["h3"],"dns":["example.com"],"ws-opts":{"path":["/ws"],"headers":{"Host":["example.com"]}}}`,
	} {
		t.Run(source, func(t *testing.T) {
			node, err := yamlx.FromJSON([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
			options, err := decodeClientOptions(node)
			if err != nil || len(model.Issues(options)) != 0 {
				t.Fatalf("valid scalar/list union rejected: %+v %v", model.Issues(options), err)
			}
			if !reflect.DeepEqual(options.ALPN.strings(), []string{"h3"}) || !reflect.DeepEqual(options.DNS.strings(), []string{"example.com"}) || options.WS == nil || firstClientList(options.WS.Path) != "/ws" || firstClientList(options.WS.Headers.Host) != "example.com" {
				t.Fatal("protocol union changed")
			}
		})
	}
}

func TestTypedClientSharedProtocolDiscriminator(t *testing.T) {
	var node model.Node
	if err := json.Unmarshal([]byte(`{"id":"owner","name":"N","type":5,"server":"example.com","port":443}`), &node); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"surge", "loon"} {
		t.Run(target, func(t *testing.T) {
			var err error
			if target == "surge" {
				_, _, err = SurgeNode(node)
			} else {
				_, err = LoonNode(node)
			}
			if err == nil || err.Error() != fmt.Sprintf(msgMihomoStoredText, kindNode, "owner", "type", "5") {
				t.Fatalf("numeric discriminator was silently ignored: %v", err)
			}
		})
	}
}

func TestTypedClientSharedCustomConfigText(t *testing.T) {
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"surge":{"custom_config":1.5},"loon":{"custom_config":2}}`), &profile); err != nil {
		t.Fatal(err)
	}
	if len(model.Issues(profile)) != 0 || profile.Surge.CustomConfig != "1.5" || profile.Loon.CustomConfig != "2" {
		t.Fatalf("shared numeric text: %+v", model.Issues(profile))
	}
}
