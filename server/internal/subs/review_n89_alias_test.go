package subs

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func TestReviewN89DNSSubtreeBudgetNeverTruncates(t *testing.T) {
	for _, field := range []string{PolicyKey, "fake-ip-filter"} {
		t.Run(field, func(t *testing.T) {
			var source strings.Builder
			source.WriteString("hosts: {source.example: target.example}\ndns:\n  enable: true\n  listen: '127.0.0.1:53'\n  proxy-server-nameserver: [https://dns.example/dns-query]\n  enhanced-mode: fake-ip\n  padding: [")
			for i := 0; i < 2000; i++ {
				source.WriteString("value,")
			}
			source.WriteString("]\n  " + field + ":\n")
			if field == PolicyKey {
				source.WriteString("    a0: &a0 [value]\n")
			} else {
				source.WriteString("    - &a0 [value]\n")
			}
			for i := 1; i <= 12; i++ {
				if field == PolicyKey {
					fmt.Fprintf(&source, "    a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
				} else {
					fmt.Fprintf(&source, "    - &a%d [*a%d, *a%d]\n", i, i-1, i-1)
				}
			}
			main, err := yamlx.Parse([]byte(source.String()))
			if err != nil {
				t.Fatal("fixture whole graph:", err)
			}
			dns := yamlx.Get(main, "dns")
			before, err := yamlx.ToJSON(dns)
			if err != nil {
				t.Fatal("fixture complete DNS must pass:", err)
			}
			if _, err := yamlx.ToJSON(yamlx.Get(dns, field)); !errors.Is(err, yamlx.ErrAliasBudget) {
				t.Fatal("fixture isolated subtree must exceed alias budget:", err)
			}
			_, err = ApplyDNS(main, yamlx.Map(yamlx.Str("source.example"), yamlx.Str("target.example")), "v1.19.20")
			if !errors.Is(err, yamlx.ErrAliasBudget) {
				t.Fatalf("expected explicit subtree budget rejection, got %v", err)
			}
			after, err := yamlx.ToJSON(yamlx.Get(main, "dns"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rejected DNS update changed original graph:", err)
			}
		})
	}
}
