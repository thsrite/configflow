package onboarding

import (
	"testing"

	"configflow/internal/yamlx"
)

func TestSharedDNSPreparationPreservesBase(t *testing.T) {
	for _, source := range []string{"", "future: &future {keep: true}\nreference: *future\ndns: {}\n", "future: keep\ndns:\n  nameserver: [tls://192.0.2.53]\n"} {
		var document *yamlx.Node
		var err error
		if source != "" {
			document, err = yamlx.Parse([]byte(source))
			if err != nil {
				t.Fatal(err)
			}
		}
		prepared, err := PrepareMihomoDNS(document, nil, noLoop)
		if err != nil {
			t.Fatal(err)
		}
		resolvers := stringList(field(prepared, "dns", "nameserver"))
		if len(resolvers) == 0 {
			t.Fatal("missing nameservers")
		}
		if source != "" && field(prepared, "future") == nil {
			t.Fatal("discarded base field")
		}
		if source != "" && field(prepared, "reference") != nil && yamlx.Resolve(field(prepared, "reference")) != yamlx.Resolve(field(prepared, "future")) {
			t.Fatal("expanded shared alias")
		}
		if source == "future: keep\ndns:\n  nameserver: [tls://192.0.2.53]\n" && resolvers[0] != "tls://192.0.2.53" {
			t.Fatal("overwrote existing resolver")
		}
	}
	a := DefaultMihomoNameservers()
	a[0] = "changed"
	if DefaultMihomoNameservers()[0] != DefaultLocalDNS {
		t.Fatal("mutable shared defaults")
	}
	if _, err := PrepareMihomoDNS(nil, nil, nil); err == nil {
		t.Fatal("missing shared checker allowed")
	}
}
