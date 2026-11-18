package oxidns

import (
	"strings"
	"testing"
)

func TestStepBC26ProtocolSchemeIsNotReference(t *testing.T) {
	for _, test := range []struct {
		text, tag string
		want      bool
	}{
		{"udp://192.0.2.53:53", "udp", false},
		{"https://dns.example/q", "https", false},
		{"foo+bar://dns.example", "foo+bar", false},
		{"udp://192.0.2.53:53 $udp", "udp", true},
		{"$udp://192.0.2.53:53", "udp", true},
		{"jump udp", "udp", true},
		{"$udp", "udp", true},
		{"udp://dns.example/udp", "udp", true},
		{"数字://dns.example", "数字", true},
	} {
		t.Run(test.text, func(t *testing.T) {
			refs := ReferenceLocations(M("value", test.text), []string{test.tag}, false)
			if got := len(refs) != 0; got != test.want {
				t.Fatalf("references=%+v want present=%v", refs, test.want)
			}
		})
	}
}

func TestStepBC26IndependentSidePolicyLifecycle(t *testing.T) {
	p := NewProfile(nil)
	draft, err := PrepareEditor(M("local_dns", "udp://192.0.2.53", "remote_dns", "https://dns.example/q"), p, "")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := LoadNativeYAML(str(get(draft, "native_config")), NativeLabel)
	if err != nil {
		t.Fatal(err)
	}
	main := false
	for _, plugin := range items(get(doc, "plugins")) {
		if str(get(plugin, "type")) == "udp_server" && str(at(plugin, "args", "entry")) == "sequence_main" {
			set(plugin, "tag", S("udp"))
			main = true
			break
		}
	}
	if !main {
		t.Fatal("fixture has no main UDP listener")
	}
	sideForward := M("tag", "side_forward", "type", "forward", "args", M("upstreams", L(M("addr", "udp://192.0.2.54:53"))))
	sideForward.HeadComment = "keep independent upstream"
	sideServer := M("tag", "side_udp", "type", "udp_server", "args", M("listen", "127.0.0.1:5353", "entry", "side_forward"))
	plugins := get(doc, "plugins")
	plugins.Content = append(plugins.Content, sideForward, sideServer)
	if err := setNativeConfig(draft, doc); err != nil {
		t.Fatal(err)
	}
	original := clone(draft)
	view, err := ReadPolicies(draft, p)
	if err != nil || !view.Editable || len(view.SideEntries) != 1 {
		t.Fatalf("independent chain refused: view=%+v err=%v", view, err)
	}
	for _, rows := range []*Node{
		L(M("ip", "192.0.2.10", "name", "synthetic", "mode", "direct")),
		L(M("ip", "192.0.2.10", "name", "synthetic", "mode", "smart")),
		L(),
	} {
		before := clone(draft)
		next, err := ApplyPolicies(draft, p, rows, "")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(str(get(next, "native_config")), "# keep independent upstream") {
			t.Fatal("side comment lost")
		}
		if !equal(draft, before) {
			t.Fatal("policy edit mutated its input")
		}
		parsed, err := LoadNativeYAML(str(get(next, "native_config")), NativeLabel)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []*Node{sideForward, sideServer} {
			found := false
			for _, plugin := range items(get(parsed, "plugins")) {
				if str(get(plugin, "tag")) == str(get(want, "tag")) {
					found = true
					if !equal(plugin, want) {
						t.Fatalf("side plugin changed: %s", ToJSON(plugin))
					}
				}
			}
			if !found {
				t.Fatal("side plugin removed")
			}
		}
		nextView, err := ReadPolicies(next, p)
		if err != nil || !nextView.Editable || len(nextView.SideEntries) != 1 || !equal(nextView.Policies, rows) {
			t.Fatalf("policy edit unreadable: %+v %v", nextView, err)
		}
		draft = next
	}
	// A real edge to the main listener must still refuse the side edit.
	set(get(sideForward, "args"), "exec", S("$udp"))
	if err := setNativeConfig(original, doc); err != nil {
		t.Fatal(err)
	}
	view, err = ReadPolicies(original, p)
	if err != nil || view.Editable || view.Reason == "" {
		t.Fatalf("real main-chain reference accepted: %+v %v", view, err)
	}
	if _, err := ApplyPolicies(original, p, L(), ""); err == nil {
		t.Fatal("unsafe edit accepted")
	}
}
