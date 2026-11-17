package subs_test

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestHostsProjectionRetainsScalarKeyTags(t *testing.T) {
	n, err := yamlx.Parse([]byte("hosts: {1: one, false: flag, null: empty, text: value}"))
	if err != nil {
		t.Fatal(err)
	}
	hosts := yamlx.Get(n, "hosts")
	if err := subs.ProjectHostsYAMLKeys(hosts); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for key := range yamlx.AllPairs(hosts) {
		tags = append(tags, yamlx.Resolve(key).ShortTag())
	}
	if strings.Join(tags, ",") != "!!int,!!bool,!!null,!!str" {
		t.Fatal(tags)
	}
	n, err = yamlx.Parse([]byte("hosts: {[a]: value}"))
	if err != nil {
		t.Fatal(err)
	}
	if err = subs.ProjectHostsYAMLKeys(yamlx.Get(n, "hosts")); !errors.Is(err, yamlx.ErrPolicy) {
		t.Fatal(err)
	}
}

func TestNativeReaderMigrationFixtures(t *testing.T) {
	projectors := map[string]func(*yamlx.Node) error{
		"parser":  func(n *yamlx.Node) error { return yamlx.ProjectJSONKeys(n, yamlx.RejectNonStringKeys) },
		"dialers": func(n *yamlx.Node) error { return yamlx.ProjectJSONKeys(n, yamlx.DiscardNonStringKeys) },
		"hosts":   func(n *yamlx.Node) error { return subs.ProjectHostsYAMLKeys(yamlx.Get(n, "hosts")) },
		"substore": func(n *yamlx.Node) error {
			p := yamlx.Get(n, "proxies")
			if p == nil {
				return nil
			}
			if err := yamlx.ProjectJSONKeys(p, yamlx.StringifyScalarKeys); err != nil {
				return err
			}
			_, err := yamlx.ToJSON(p)
			return err
		},
		"detection":   func(*yamlx.Node) error { return nil },
		"core-corpus": func(*yamlx.Node) error { return nil },
		"core-review": func(*yamlx.Node) error { return nil },
	}
	yamlxtest.CheckReaderFixtures(t, "testdata/n7_native_values.json.gz", "testdata/n7_reader_expected.json.gz", projectors)
}
