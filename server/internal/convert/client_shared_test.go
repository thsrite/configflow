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
			if err != nil || len(model.Issues(options)) != 0 {
				t.Fatalf("valid scalar/list union rejected: %+v %v", model.Issues(options), err)
			}
			if !reflect.DeepEqual(options.ALPN.strings(), []string{"h3"}) || !reflect.DeepEqual(options.DNS.strings(), []string{"example.com"}) || options.WS == nil || firstClientList(options.WS.Path) != "/ws" || firstClientList(options.WS.Headers.Host) != "example.com" {
				t.Fatal("protocol union changed")
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
