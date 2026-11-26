package rules

import (
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
)

// syntheticRuleset is the input testdata/bench.py builds too: 300000 lines of
// classical rules (suffix, exact, wildcard, CIDR, a few keywords).
func syntheticRuleset() string {
	var b strings.Builder
	for i := 0; i < 300_000; i++ {
		switch i % 10 {
		case 0, 1, 2, 3, 4:
			fmt.Fprintf(&b, "DOMAIN-SUFFIX,site%d.example\n", i)
		case 5, 6, 7:
			fmt.Fprintf(&b, "DOMAIN,host%d.example.org\n", i)
		case 8:
			fmt.Fprintf(&b, "IP-CIDR,10.%d.%d.0/24,no-resolve\n", i/256%256, i%256)
		default:
			if i%1000 == 9 {
				fmt.Fprintf(&b, "DOMAIN-KEYWORD,kw%d\n", i)
			} else {
				fmt.Fprintf(&b, "DOMAIN-SUFFIX,other%d.test\n", i)
			}
		}
	}
	return b.String()
}

func benchMatcher(body string) *Matcher {
	beh := "classical"
	return NewMatcher(
		[]model.RuleConfig{{ID: "set", ItemType: "ruleset", Behavior: &beh, Policy: strp("PROXY")}},
		nil,
		func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) { return body, nil },
		nil,
		nil,
	)
}

func BenchmarkIndexBuild300kLines(b *testing.B) {
	body := syntheticRuleset()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := benchMatcher(body)
		m.Match("warmup.test")
	}
}

func BenchmarkMatch300kLines(b *testing.B) {
	m := benchMatcher(syntheticRuleset())
	m.Match("warmup.test")
	queries := []string{"www.site4000.example", "a.b.other3999.test", "nothing.test", "10.5.5.9", "host4005.example.org", "zz.kw9009.com"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Match(queries[i%len(queries)])
	}
}
