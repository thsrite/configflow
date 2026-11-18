package subs

import (
	"configflow/internal/yamlx"
	"testing"
)

func TestN89NativeHostResolver(t *testing.T) {
	root, err := yamlx.FromJSON([]byte(`{"A.Example.":"b.example","b.example":["target.example"],"ip.example":["192.0.2.1"],"cycle.example":"cycle.example","+.suffix.example":"broad.example","*.deep.suffix.example":"specific.example","null.example":null}`))
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewHostResolver(root)
	cases := map[string]string{"a.example": "target.example", "absent.example": "", "ip.example": "", "cycle.example": "", "n.suffix.example": "broad.example", "n.deep.suffix.example": "specific.example", "null.example": "", "A.Example": ""}
	for domain, want := range cases {
		if got := resolver.Target(domain); got != want {
			t.Fatalf("%s: %q want %q", domain, got, want)
		}
	}
}

func TestN89NativeMappedAddressProperties(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"::ffff:192.0.2.1", "::ffff:192.0.2.1"}, {"::ffff:127.0.0.1", "::ffff:127.0.0.1"}, {"::ffff:224.0.0.1", ""}, {"::ffff:0.0.0.0", ""}, {"2001:0db8:0:0::1", "2001:db8::1"}, {"fe80::1%eth0", "fe80::1%eth0"},
	} {
		if got := usableAddress(tc.input); got != tc.want {
			t.Fatalf("%s: %q want %q", tc.input, got, tc.want)
		}
	}
}

func TestN89HostChainEndsAtNullMapping(t *testing.T) {
	hosts := yamlx.Map(yamlx.Str("source.example"), yamlx.Str("target.example"), yamlx.Str("*.example"), yamlx.Null())
	resolver := NewHostResolver(hosts)
	if got := resolver.Target("source.example"); got != "target.example" {
		t.Fatalf("target %q", got)
	}
	if got := resolver.Target("null.example"); got != "" {
		t.Fatalf("initial null target %q", got)
	}
}
