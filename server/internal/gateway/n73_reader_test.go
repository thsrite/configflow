package gateway

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func collectTextFields(value any, name string, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if k == name {
				if text, ok := item.(string); ok {
					*out = append(*out, text)
				}
			}
			collectTextFields(item, name, out)
		}
	case []any:
		for _, item := range v {
			collectTextFields(item, name, out)
		}
	}
}

func TestN73GatewayReaderMigration(t *testing.T) {
	var bases, payloads []string
	addPayload := func(source string) {
		if strings.ContainsAny(source, "\x00\ufffd") || !utf8.ValidString(source) || len(source) > maxContentBytes {
			return
		}
		stripped := strings.TrimLeft(source, "\ufeff\r\n \t")
		if isYAMLContent(stripped) {
			payloads = append(payloads, stripped)
		}
	}
	file, err := os.Open("testdata/a19_corpus.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		args, _ := row["args"].(map[string]any)
		if config, ok := args["config"].(map[string]any); ok {
			if mihomo, ok := config["mihomo"].(map[string]any); ok {
				if source, ok := mihomo["custom_config"].(string); ok && strings.TrimSpace(source) != "" {
					bases = append(bases, source)
				}
			}
			var contents []string
			collectTextFields(config, "content", &contents)
			for _, source := range contents {
				addPayload(source)
			}
		}
		if fn, _ := row["fn"].(string); fn == "extract" {
			if content, ok := args["content"].(string); ok {
				addPayload(content)
			}
		}
		if loader, ok := args["loader"].(map[string]any); ok {
			for _, raw := range loader {
				if source, ok := raw.(string); ok {
					addPayload(source)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 || len(payloads) == 0 {
		t.Fatal("real gateway reader sources missing")
	}
	yamlxtest.Check(t, "gateway/base", bases)
	yamlxtest.Check(t, "gateway/payload", payloads)
}

func TestN73GatewayRejectsUnsafeAliases(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("l0: &l0 [entry]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&chain, "l%d: &l%d [*l%d, *l%d]\n", i, i, i-1, i-1)
	}
	for name, source := range map[string]string{"depth40": chain.String(), "self": "loop: &loop [*loop]\n"} {
		t.Run(name, func(t *testing.T) {
			config := NewFixtureConfig(map[string]any{"mihomo": map[string]any{"custom_config": source}})
			if doc, err := config.BaseDocument(); doc != nil || err == nil || err.Error() != msgBaseYAMLInvalid {
				t.Fatalf("unsafe base document accepted or context changed: %v", err)
			}
			payload := "payload: ['IP-CIDR,192.0.2.0/24']\n" + source
			if values, err := yamlPayloadStrings(payload); values != nil || err == nil || err.Error() != msgYAMLSyntax {
				t.Fatalf("unsafe payload accepted or context changed: %v", err)
			}
			if prefixes, err := ExtractCIDRs(payload, "classical"); prefixes != nil || err == nil {
				t.Fatal("unsafe graph produced a gateway capture")
			}
		})
	}
}

func TestN73GatewayBasePreservesSharing(t *testing.T) {
	config := NewFixtureConfig(map[string]any{"mihomo": map[string]any{"custom_config": "base: &base [one, two]\nfirst: *base\nsecond: *base\n"}})
	n, err := config.BaseDocument()
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Resolve(yamlx.Get(n, "first")) != yamlx.Resolve(yamlx.Get(n, "second")) {
		t.Fatal("base reader expanded shared values")
	}
}
