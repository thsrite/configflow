package subs

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

// Ports of backend/test_mihomo_listener_lifecycle.py. The cases that go
// through the proxy-group routes (test_group_edit_relinks_only_fixed_inbounds_in_same_transaction,
// test_other_references_still_block_deletion_without_changing_listener,
// test_roundtrip_failure_does_not_partially_rename_or_rewrite_profile) and
// through generate_mihomo_config belong to C33 and the converter work; their listener-level
// assertions are covered here against RelinkListeners and
// ValidateListenerNode directly.

func strp(s string) *string { return &s }

func relink(t *testing.T, source string, newName *string) (*model.Profile, []string, error) {
	t.Helper()
	profile := &model.Profile{Mihomo: model.MihomoSettings{CustomConfig: source}}
	changed, err := RelinkListeners(profile, "Pinned", newName)
	return profile, changed, err
}

func mustListenerParse(t *testing.T, source string) *yamlx.Node {
	t.Helper()
	n, err := yamlx.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func expectEdited(t *testing.T, source string, newName *string) *yamlx.Node {
	t.Helper()
	doc := yamlx.Clone(mustListenerParse(t, source))
	listener := yamlx.Get(doc, "listeners").Content[0]
	if newName == nil {
		yamlx.Delete(listener, "proxy")
	} else {
		yamlx.Set(listener, "proxy", yamlx.Str(*newName))
	}
	return doc
}

func TestListenerEditDoesNotChangeSharedYamlAliasOrUnrelatedDocument(t *testing.T) {
	source := "shared: &base {name: pinned, type: mixed, proxy: Pinned}\nlisteners: [*base]\n"
	profile, changed, err := relink(t, source, nil)
	if err != nil || strings.Join(changed, ",") != "listeners[0].proxy" {
		t.Fatalf("changed %v err %v", changed, err)
	}
	document := mustListenerParse(t, profile.Mihomo.CustomConfig)
	if yamlx.Get(yamlx.Get(document, "shared"), "proxy").Value != "Pinned" {
		t.Fatal("shared alias changed")
	}
	if yamlx.Get(yamlx.Get(document, "listeners").Content[0], "proxy") != nil {
		t.Fatal("proxy kept")
	}
	unrelated, changed, err := relink(t, "# keep this comment\nlisteners: []\n", nil)
	if err != nil || len(changed) != 0 || unrelated.Mihomo.CustomConfig != "# keep this comment\nlisteners: []\n" {
		t.Fatalf("unrelated changed: %v %v %q", changed, err, unrelated.Mihomo.CustomConfig)
	}
}

func TestCommentEditsKeepAliasConsumersAndFlowOrMergeSemantics(t *testing.T) {
	sources := []string{
		"# top\nshared: &base {name: pinned, type: mixed, proxy: Pinned} # base\nlisteners: [*base] # list\n",
		"# top\nlisteners: [&base {name: pinned, type: mixed, proxy: Pinned}] # list\nshared: *base # shared\n",
		"# top\nshared: &base {name: pinned, type: mixed, proxy: Pinned} # base\nlisteners:\n- <<: *base # merge\n  port: 17890 # port\n",
		"# top\nlisteners: [{proxy: Pinned, name: pinned}] # list\n",
		"# top\nlisteners: &all [{name: pinned, proxy: Pinned}] # list\nshared: *all # shared\n",
		"# top\nlisteners: [{name: pinned, proxy: &policy Pinned}] # list\nshared: *policy # shared\n",
		"# top\nlisteners: [{proxy: Pinned, # policy note\n name: pinned}] # list\n",
	}
	names := []*string{nil, strp(`Renamed # : "quoted"`), strp("false"), strp("on"), strp("No"), strp("012")}
	for si, source := range sources {
		for _, name := range names {
			label := "nil"
			if name != nil {
				label = fmt.Sprintf("%q", *name)
			}
			t.Run(fmt.Sprintf("%d/%s", si, label), func(t *testing.T) {
				expected := expectEdited(t, source, name)
				profile, changed, err := relink(t, source, name)
				if err != nil || strings.Join(changed, ",") != "listeners[0].proxy" {
					t.Fatalf("changed %v err %v (%v)", changed, err, causeOf(err))
				}
				result := profile.Mihomo.CustomConfig
				if !yamlx.Equal(mustListenerParse(t, result), expected) {
					t.Fatalf("result does not load to the expected document:\n%s", result)
				}
				for _, c := range []string{"# top", "# base", "# list", "# shared", "# merge", "# port", "# policy note"} {
					if strings.Contains(source, c) && !strings.Contains(result, c) {
						t.Fatalf("comment %q lost:\n%s", c, result)
					}
				}
			})
		}
	}
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func TestCommentEditKeepsScalarTypesLiteralContentAndWindowsNewlines(t *testing.T) {
	source := "# header\r\nlegacy: {enabled: on, number: 012, flag: false, empty: null}\r\n" +
		"script: |\r\n  # This is string content\r\n  proxy: Pinned\r\n" +
		"listeners:\r\n- {name: pinned, proxy: \"Pinned\", type: mixed}\r\n"
	expected := expectEdited(t, source, strp("Renamed"))
	profile, _, err := relink(t, source, strp("Renamed"))
	if err != nil {
		t.Fatal(err, causeOf(err))
	}
	result := profile.Mihomo.CustomConfig
	if !yamlx.Equal(mustListenerParse(t, result), expected) {
		t.Fatalf("result:\n%q", result)
	}
	if !strings.Contains(result, "# header") || !strings.Contains(result, "# This is string content") {
		t.Fatalf("comments lost: %q", result)
	}
	if strings.Contains(strings.ReplaceAll(result, "\r\n", ""), "\n") {
		t.Fatalf("bare line feed: %q", result)
	}
}

func TestGroupEditsPreserveAllBaseYamlCommentsAfterSave(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 230; i++ {
		fmt.Fprintf(&b, "# User explanation %d\n", i)
	}
	source := b.String() + `mixed-port: 7890 # mixed port
listeners:
  # inbound explanation
  - name: 'personal' # listener name
    type: mixed
    proxy: "Pinned" # fixed policy explanation
    # rule explanation
    rule: lan-rules
    port: 17890 # listener port
sub-rules: {lan-rules: [MATCH,DIRECT]} # sub-rule explanation
`
	// rename, then disable/delete (both drop the fixed target)
	for _, name := range []*string{strp("Renamed"), nil} {
		profile, _, err := relink(t, source, name)
		if err != nil {
			t.Fatal(err, causeOf(err))
		}
		result := profile.Mihomo.CustomConfig
		want := []string{"# mixed port", "# inbound explanation", "# listener name", "# fixed policy explanation",
			"# rule explanation", "# listener port", "# sub-rule explanation"}
		for i := 0; i < 230; i++ {
			want = append(want, fmt.Sprintf("# User explanation %d", i))
		}
		for _, c := range want {
			if !strings.Contains(result, c) {
				t.Fatalf("%v: comment %q lost", deref(name), c)
			}
		}
		if !yamlx.Equal(mustListenerParse(t, result), expectEdited(t, source, name)) {
			t.Fatalf("%v: result:\n%s", deref(name), result)
		}
	}
}

// The listener part of test_uncertain_native_config_never_partially_deletes_group:
// the relink refuses (the route answers 400 and writes nothing).
func TestUncertainNativeConfigIsRefused(t *testing.T) {
	for _, source := range []string{"listeners: [", "listeners: {proxy: Pinned}",
		`listeners: [{name: custom, proxy: "${FIXED_POLICY}"}]`} {
		profile, _, err := relink(t, source, nil)
		var le *ListenerReferenceError
		if !errors.As(err, &le) {
			t.Fatalf("%q: err %v", source, err)
		}
		var se *rules.StrategyReferenceError
		if !errors.As(err, &se) {
			t.Fatalf("%q: not a StrategyReferenceError", source)
		}
		if profile.Mihomo.CustomConfig != source {
			t.Fatalf("%q: rewritten", source)
		}
	}
}

func listenerDocument(t *testing.T, target any) *yamlx.Node {
	t.Helper()
	var proxyNode *yamlx.Node
	if target == nil {
		proxyNode = yamlx.Null()
	} else {
		proxyNode = yamlx.Str(target.(string))
	}
	listener := yamlx.Map(
		yamlx.Str("name"), yamlx.Str("pinned"),
		yamlx.Str("type"), yamlx.Str("mixed"),
		yamlx.Str("listen"), yamlx.Str("127.0.0.1"),
		yamlx.Str("port"), yamlx.Int(17890),
		yamlx.Str("proxy"), proxyNode,
		yamlx.Str("rule"), yamlx.Str("lan-rules"),
		yamlx.Str("future-field"), yamlx.Seq(yamlx.Str("preserved")),
	)
	doc := yamlx.Map(
		yamlx.Str("dns"), yamlx.Map(yamlx.Str("enable"), yamlx.Bool(false)),
		yamlx.Str("future-setting"), yamlx.Map(yamlx.Str("keep"), yamlx.Bool(false)),
		yamlx.Str("listeners"), yamlx.Seq(
			listener,
			yamlx.Map(
				yamlx.Str("name"), yamlx.Str("direct"),
				yamlx.Str("type"), yamlx.Str("mixed"),
				yamlx.Str("port"), yamlx.Int(17891),
				yamlx.Str("proxy"), yamlx.Str("DIRECT"),
			),
		),
		yamlx.Str("sub-rules"), yamlx.Map(yamlx.Str("lan-rules"), yamlx.Seq(yamlx.Str("MATCH,DIRECT"))),
		yamlx.Str("proxy-groups"), yamlx.Seq(yamlx.Map(
			yamlx.Str("name"), yamlx.Str("Pinned"),
			yamlx.Str("type"), yamlx.Str("select"),
			yamlx.Str("proxies"), yamlx.Seq(yamlx.Str("DIRECT")),
		)),
	)
	// a YAML round trip, as the converter reads the base config
	text, err := yamlx.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	node, err := readSubscriptionYAMLNode(string(text), false)
	if err != nil {
		t.Fatal(err)
	}
	return node
}

// The validation part of test_generation_rejects_missing_listener_target_without_dialer_topology.
func TestGenerationRejectsMissingListenerTargetWithoutDialerTopology(t *testing.T) {
	for _, target := range []string{"Missing", "${FIXED_POLICY}", " Pinned "} {
		err := ValidateListenerNode(listenerDocument(t, target))
		if err == nil || !strings.Contains(err.Error(), "listeners[0].proxy") {
			t.Fatalf("%q: %v", target, err)
		}
	}
}

// The validation part of test_generation_keeps_valid_or_unpinned_listener_targets.
func TestGenerationKeepsValidOrUnpinnedListenerTargets(t *testing.T) {
	for _, target := range []any{"", nil, "DIRECT", "GLOBAL", "Pinned"} {
		if err := ValidateListenerNode(listenerDocument(t, target)); err != nil {
			t.Fatalf("%v: %v", target, err)
		}
	}
}

func TestListenerDuplicateKeysAndCRLFRegression(t *testing.T) {
	source := "# header\r\nlisteners:\r\n- name: first # first\r\n  proxy: Pinned # fixed\r\n\r\n# gap\r\n- name: second\r\n  proxy: DIRECT\r\n"
	profile, changed, err := relink(t, source, strp("Renamed"))
	if err != nil || len(changed) != 1 {
		t.Fatalf("%v %v", changed, err)
	}
	if got := profile.Mihomo.CustomConfig; got != strings.Replace(source, "proxy: Pinned", "proxy: Renamed", 1) {
		t.Fatalf("CRLF splice changed layout: %q", got)
	}
	duplicate := "listeners:\n- name: one\n  proxy: Pinned\n  proxy: DIRECT\n"
	profile, _, err = relink(t, duplicate, nil)
	var detail *yamlx.DuplicateKeyError
	if !errors.Is(err, yamlx.ErrDuplicateKey) || !errors.As(err, &detail) || detail.Key != "proxy" || detail.Line != 4 || !strings.HasPrefix(err.Error(), msgListenerBaseParse) {
		t.Fatalf("wrong duplicate diagnostic: %v", err)
	}
	if profile.Mihomo.CustomConfig != duplicate {
		t.Fatal("duplicate source changed")
	}
}
