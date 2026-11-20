package rulefetch

import "testing"

// Regression (review N2): rule source URLs requests could fetch stay valid:
// credentials with characters net/url refuses, a stray "%", Chinese paths.
func TestReviewN2RuleURLs(t *testing.T) {
	for _, raw := range []string{
		"https://u:p^w@rules.example.com/list.yaml",
		"https://rules.example.com/规则/100%.list",
		"https://token@rules.example.com/a%2Fb.yaml?x=中",
	} {
		if _, err := ValidateRuleURL(raw); err != nil {
			t.Errorf("%q refused: %v", raw, err)
		}
	}
	for _, raw := range []string{"https://u:p w@rules.example.com/x", "ftp://rules.example.com/x", "https:///x", "https://h:0/x"} {
		if _, err := ValidateRuleURL(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
	if got, err := NormalizeProxy(" HTTP://User:p^w@Proxy.example:7890/ "); err != nil || got != "http://User:p^w@Proxy.example:7890" {
		t.Errorf("proxy %q %v", got, err)
	}
	if got, err := NormalizeProxy("http://User:pa%20ss@Proxy.example:7890"); err != nil || got != "http://User:pa%20ss@Proxy.example:7890" {
		t.Errorf("encoded proxy password space %q %v", got, err)
	}
	for _, raw := range []string{"http://p:1/x", "http://p:1/?a", "http://p:1#f"} {
		if _, err := NormalizeProxy(raw); err != ErrInvalidProxy {
			t.Errorf("%q: %v", raw, err)
		}
	}
}
