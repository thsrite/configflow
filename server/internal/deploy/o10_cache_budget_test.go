package deploy

import (
	"fmt"
	"strings"
	"testing"
)

func TestConversionsCacheBudget(t *testing.T) {
	clearConversions := func() {
		conversions.Lock()
		conversions.values = nil
		conversions.bytes = 0
		conversions.Unlock()
	}
	clearConversions()
	t.Cleanup(clearConversions)

	oversized := []byte(strings.Repeat("example.com\n", (conversionsCacheMaxItemLen/12)+10))
	got, err := ConvertMosDNSRules(oversized, "domain_set", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) <= conversionsCacheMaxItemLen {
		t.Fatalf("expected oversized result > %d, got %d", conversionsCacheMaxItemLen, len(got))
	}
	conversions.Lock()
	afterOversized := len(conversions.values)
	conversions.Unlock()
	if afterOversized != 0 {
		t.Fatalf("oversized conversion retained: %d entries", afterOversized)
	}

	unit := strings.Repeat("a.example.com\n", 15000)
	for i := range 8 {
		body := []byte(fmt.Sprintf("# %d\n%s", i, unit))
		if _, err := ConvertMosDNSRules(body, "domain_set", false); err != nil {
			t.Fatal(err)
		}
	}
	conversions.Lock()
	retainedBytes := conversions.bytes
	conversions.Unlock()
	if retainedBytes > conversionsCacheMaxBytes {
		t.Fatalf("conversions retained %d bytes > limit %d", retainedBytes, conversionsCacheMaxBytes)
	}
}
