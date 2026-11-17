package importer_test

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/importer"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func TestN73ImporterReaderMigration(t *testing.T) {
	file, err := os.Open("testdata/corpus.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	classes := map[string]int{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(nil, 4<<20)
	for scanner.Scan() {
		var row corpusRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		if row.Client != "mihomo" || strings.TrimSpace(row.Content) == "" {
			continue
		}
		category, err := yamlxtest.CompareNativeReader(strings.TrimLeft(row.Content, "\ufeff"), yamlxtest.NativeDecoded)
		if err != nil {
			t.Fatalf("real importer source %s: %v", row.Name, err)
		}
		classes[category]++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if classes["equal"] == 0 {
		t.Fatal("no real Mihomo importer inputs compared")
	}
	t.Logf("native reader classes=%v unknown=0", classes)
}

func TestN73ImporterRejectsUnsafeAliases(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	for _, test := range []struct {
		name, source string
		want         error
	}{
		{"depth40", chain.String(), yamlx.ErrAliasBudget},
		{"self", "loop: &loop [*loop]\n", yamlx.ErrAliasRecursion},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := importer.Parse("mihomo", test.source+"proxies: [{name: Node, type: http, server: example.com, port: 80}]\n")
			if !errors.Is(err, test.want) || !strings.HasPrefix(err.Error(), "YAML 解析失败：") {
				t.Fatalf("unsafe graph was not rejected with its context: %v", err)
			}
		})
	}
}

func TestN73ImporterRetainsSharedCustomValues(t *testing.T) {
	source := "proxies: [{name: Node, type: http, server: example.com, port: 80}]\nbase: &base [first, second]\nuse: *base\n"
	plan, err := importer.Parse("mihomo", source)
	if err != nil || plan.Custom == nil {
		t.Fatalf("import: %v", err)
	}
	// Build exposes the retained custom YAML at the actual public boundary.
	result, err := importer.Build(plan, importer.Shared{}, importer.BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Profile struct {
			Mihomo struct {
				Custom string `json:"custom_config"`
			} `json:"mihomo"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	n, err := yamlx.Parse([]byte(response.Profile.Mihomo.Custom))
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Resolve(yamlx.Get(n, "base")) != yamlx.Resolve(yamlx.Get(n, "use")) {
		t.Fatal("retained custom configuration expanded its shared value")
	}
}

func TestN73ImporterSelectedSubtreeBudget(t *testing.T) {
	var prefix strings.Builder
	prefix.WriteString("proxy-providers:\n  padding:\n    content: [")
	prefix.WriteString(strings.Repeat("item,", 2048))
	prefix.WriteString("]\nl0: &l0 [entry]\n")
	for i := 1; i <= 12; i++ {
		fmt.Fprintf(&prefix, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	for _, option := range []string{"", ", extra: *l12"} {
		source := prefix.String() + "proxies: [{name: Node, type: http, server: example.com, port: 80" + option + "}]\n"
		if _, err := yamlx.Parse([]byte(source)); err != nil {
			t.Fatalf("fixture must fit the full-document budget: %v", err)
		}
		if plan, err := importer.Parse("mihomo", source); !errors.Is(err, yamlx.ErrAliasBudget) || plan != nil {
			t.Fatalf("selected custom/JSON subtree exceeded budget without rejection: %v", err)
		}
	}
}
