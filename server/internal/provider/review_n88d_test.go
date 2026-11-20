package provider

import (
	"fmt"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func TestReviewN88dReferenceBindings(t *testing.T) {
	rows := loadCorpus(t)
	refs := nativeDeliveryReferences(t, rows)
	checked := 0
	for index, expected := range refs {
		row := rows[index]
		if row.GetString("kind") != "snapshot" {
			continue
		}
		checked++
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			got := snapshotRow(row.GetObject("input"))
			if !jsonx.Equal(expected, got) {
				t.Fatal(diffText(expected, got))
			}
		})
	}
	if checked != 75 {
		t.Fatalf("snapshot references=%d", checked)
	}
}

func TestReviewN88dExternalProviderCompatibility(t *testing.T) {
	for _, source := range []string{
		"proxies: [{name: Node, type: http, server: example.com, port: '80', extension: {7: value}}]",
		"template: &p {type: vless, server: example.com, port: 443, uuid: synthetic}\nproxies: [{<<: *p, name: Node}]",
		"proxies: [{name: Node, type: future-protocol, opaque: [true, 7, null]}]\nunrelated: {[complex, key]: value}",
	} {
		t.Run(source, func(t *testing.T) {
			nodes, err := ParseNodes(source, "Feed", "")
			if err != nil || len(nodes) != 1 || memberText(nodes[0], "name") != "Node" {
				t.Fatalf("compatible source rejected: %v", err)
			}
			if _, err := yamlx.Marshal(yamlx.Map(yamlx.Str("proxies"), yamlx.Seq(nodes...))); err != nil {
				t.Fatal(err)
			}
		})
	}
}
