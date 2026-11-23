package convert

import (
	"encoding/json"
	"reflect"
	"strings"
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
			if err != nil {
				t.Fatalf("valid scalar/list union rejected: %v", err)
			}
			if !reflect.DeepEqual(options.ALPN.strings(), []string{"h3"}) || !reflect.DeepEqual(options.DNS.strings(), []string{"example.com"}) || options.WS == nil || firstClientList(options.WS.Path) != "/ws" || firstClientList(options.WS.Headers.Host) != "example.com" {
				t.Fatal("protocol union changed")
			}
		})
	}
}

// TestTypedClientSharedCustomConfigText: a client base stored as a number is
// damaged (O18 T1d review M3 ruling; Python failed on it), zero is empty.
func TestTypedClientSharedCustomConfigText(t *testing.T) {
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"surge":{"custom_config":1.5},"loon":{"custom_config":0}}`), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Surge.CustomConfig != model.DamagedText || profile.Loon.CustomConfig != "" {
		t.Fatalf("shared numeric text: %q %q", profile.Surge.CustomConfig, profile.Loon.CustomConfig)
	}
}

func TestTypedClientLegacyRuleListShape(t *testing.T) {
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"rule_sets":false}`), &profile); err != nil {
		t.Fatal(err)
	}
	_, _, err := MihomoRuleLists(&model.Config{Profile: &profile})
	if err == nil || !strings.Contains(err.Error(), msgMihomoRules) {
		t.Fatalf("invalid open rule list was not diagnosed: %v", err)
	}
}
