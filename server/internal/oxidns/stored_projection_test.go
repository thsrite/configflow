package oxidns

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestStoredLegacySettingsAgainstPython(t *testing.T) {
	oracle := nativeOracle(t)
	for _, tt := range []struct {
		name, key string
		value     *Node
		issue     string
		different bool
	}{
		{"disabled", "enabled", I(0), "oxidns.custom_matches[0].enabled", false},
		{"enabled", "enabled", I(1), "oxidns.custom_matches[0].enabled", false},
		{"numeric table", "routing_table", I(7), "oxidns.ros_route.args.routing_table", true},
		{"numeric gateway", "gateway4", I(7), "oxidns.ros_route.args.gateway4", true},
		{"array table", "routing_table", L("main"), "oxidns.ros_route.args.routing_table", true},
		{"object gateway", "gateway4", M("invalid", "shape"), "oxidns.ros_route.args.gateway4", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			settings := DefaultSettings()
			if tt.key == "enabled" {
				set(settings, "custom_matches", L(M("enabled", tt.value, "matches", L("qname example.com"), "exec", "accept")))
			} else {
				route := get(settings, "ros_route")
				set(route, "enabled", B(true))
				args := get(route, "args")
				set(args, "address", S("192.0.2.1:8728"))
				set(args, "username", S("synthetic-user"))
				set(args, "password", S("synthetic-password"))
				set(args, "routing_table", S("main"))
				set(args, "gateway4", S("192.0.2.254"))
				set(args, tt.key, tt.value)
			}
			before := ToJSON(settings)
			if _, err := NormalizeSettings(settings, true); err == nil {
				t.Fatal("request accepted legacy non-native type")
			}
			var profile model.Profile
			if err := profile.OxiDNS.SetNode(settings); err != nil {
				t.Fatal(err)
			}
			source := &model.Config{Profile: &profile}
			storedBefore, err := profile.OxiDNS.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			view, err := NewTypedProfile(source)
			if err != nil {
				t.Fatal(err)
			}
			if issues := view.FieldIssues(); len(issues) != 1 || issues[0].Path != tt.issue || issues[0].Kind != model.IssueType {
				t.Fatalf("diagnostic=%v", issues)
			}
			got, err := Generate(view, "")
			if err != nil {
				t.Fatal(err)
			}
			reference := func(node *Node) string {
				input := M("fn", "generate", "args", L(M("oxidns", node), S("")))
				output := oracle.Call(t, ToJSON(input), nil)
				var answer struct {
					Out string   `json:"out"`
					Err []string `json:"err"`
				}
				if err := json.Unmarshal(output, &answer); err != nil {
					t.Fatal(err)
				}
				if answer.Err != nil {
					t.Fatalf("Python generation: %v", answer.Err)
				}
				return answer.Out
			}
			pythonOriginal := reference(settings)
			if same := yamlxtest.SemanticEqual(got, pythonOriginal); same == tt.different {
				t.Fatalf("original Python difference=%v, expected=%v", !same, tt.different)
			}
			projected := clone(settings)
			if tt.key == "enabled" {
				set(get(projected, "custom_matches").Content[0], "enabled", B(tt.value.Value == "1"))
			} else if isNum(tt.value) {
				set(at(projected, "ros_route", "args"), tt.key, S(tt.value.Value))
			} else {
				set(get(projected, "ros_route"), "enabled", B(false))
				set(at(projected, "ros_route", "args"), tt.key, S(""))
			}
			pythonProjected := reference(projected)
			if !yamlxtest.SemanticEqual(got, pythonProjected) {
				t.Fatalf("native contract differs from Python projection:\n%s\n%s", got, pythonProjected)
			}
			storedAfter, err := profile.OxiDNS.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(storedBefore) != string(storedAfter) || string(before) != string(ToJSON(settings)) {
				t.Fatal("reading legacy settings modified source")
			}
		})
	}
	t.Run("invalid UDP entry cannot create TCP", func(t *testing.T) {
		profile := emptyProfile()
		source, err := Generate(profile, "")
		if err != nil {
			t.Fatal(err)
		}
		document, err := LoadNativeYAML(source, "完整配置")
		if err != nil {
			t.Fatal(err)
		}
		plugins := get(document, "plugins")
		var retained []*Node
		var udp *Node
		for _, plugin := range plugins.Content {
			if str(get(plugin, "tag")) == "server_tcp" {
				continue
			}
			if str(get(plugin, "tag")) == "server_udp" {
				udp = plugin
			}
			retained = append(retained, plugin)
		}
		plugins.Content = retained
		set(get(udp, "args"), "entry", I(5))
		data, err := yamlx.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		settings := DefaultSettings()
		set(settings, "config_mode", S("native"))
		set(settings, "native_config", S(string(data)))
		view, err := ReadCommonSettings(settings, profile, "")
		if err != nil {
			t.Fatal(err)
		}
		cap := view.Capabilities["tcp_enabled"]
		if cap.Editable || cap.Reason != messageTCPPrerequisites {
			t.Fatalf("unsafe TCP capability: %+v", cap)
		}
		output := oracle.Call(t, ToJSON(M("fn", "read_common_settings", "args", L(settings, M(), S("")))), nil)
		var reference struct {
			Out struct {
				Capabilities map[string]struct {
					Editable bool `json:"editable"`
				} `json:"capabilities"`
			} `json:"out"`
			Err []string `json:"err"`
		}
		if err := json.Unmarshal(output, &reference); err != nil {
			t.Fatal(err)
		}
		if reference.Err != nil || !reference.Out.Capabilities["tcp_enabled"].Editable {
			t.Fatalf("Python unsafe capability no longer reproduced: %s", output)
		}
		set(get(udp, "args"), "entry", S("sequence_main"))
		data, err = yamlx.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		set(settings, "native_config", S(string(data)))
		view, err = ReadCommonSettings(settings, profile, "")
		if err != nil || !view.Capabilities["tcp_enabled"].Editable {
			t.Fatalf("valid TCP source rejected: %v", err)
		}
	})

}

func TestNativeRequestExpectationControls(t *testing.T) {
	args := []json.RawMessage{json.RawMessage(`{"custom_matches":[{"matches":[],"exec":"accept","enabled":0}]}`), json.RawMessage(`true`)}
	r := row{Fn: "normalize", Args: args}
	if !nativeRequestExpectation(t, r, fail(MatchEnabledTypeMessage)) {
		t.Fatal("registered native rejection missing")
	}
	if nativeRequestExpectation(t, r, nil) || nativeRequestExpectation(t, r, fmt.Errorf("unrelated")) {
		t.Fatal("native expectation swallowed success/error")
	}
	r.Args[0] = json.RawMessage(`{"custom_matches":[{"matches":[],"exec":"changed","enabled":0}]}`)
	if nativeRequestExpectation(t, r, fail(MatchEnabledTypeMessage)) {
		t.Fatal("unregistered input accepted")
	}
	r.Fn = "generate"
	if nativeRequestExpectation(t, r, fail(MatchEnabledTypeMessage)) {
		t.Fatal("other function accepted")
	}
	// Shared Node scalar tags remain distinct after the new boundary projection.
	if yamlx.Equal(I(1), B(true)) || yamlx.Equal(S("1"), I(1)) {
		t.Fatal("strict scalar comparison lost")
	}
	if !strings.Contains(RouteFieldTextMessage, "%s") {
		t.Fatal("route message lost field context")
	}
}
