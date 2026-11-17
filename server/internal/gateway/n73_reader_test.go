package gateway

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

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
	for _, row := range yamlxtest.Rows(t, "testdata/a19_corpus.jsonl.gz") {
		args := row.GetObject("args")
		if config := args.GetObject("config"); config != nil {
			if mihomo := config.GetObject("mihomo"); mihomo != nil {
				if source := mihomo.GetString("custom_config"); strings.TrimSpace(source) != "" {
					bases = append(bases, source)
				}
			}
			for _, source := range yamlxtest.TextFields(config, "content") {
				addPayload(source)
			}
		}
		if row.GetString("fn") == "extract" {
			addPayload(args.GetString("content"))
		}
		if loader := args.GetObject("loader"); loader != nil {
			for _, raw := range loader.All() {
				if source, ok := raw.(string); ok {
					addPayload(source)
				}
			}
		}
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
			config := NewConfig(jsonx.Obj("mihomo", jsonx.Obj("custom_config", source)))
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
	config := NewConfig(jsonx.Obj("mihomo", jsonx.Obj("custom_config", "base: &base [one, two]\nfirst: *base\nsecond: *base\n")))
	n, err := config.BaseDocument()
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Resolve(yamlx.Get(n, "first")) != yamlx.Resolve(yamlx.Get(n, "second")) {
		t.Fatal("base reader expanded shared values")
	}
}
