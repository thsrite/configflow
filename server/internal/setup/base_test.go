package setup

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func identityDNS() *DNSOwner {
	return &DNSOwner{DefaultLocalDNS: "https://dns.example.com/dns-query", DNSTarget: "127.0.0.1:1053", Nameservers: func() []string { return []string{"https://dns.example.com/dns-query"} }, LocalUpstreams: func(string, bool) (*yamlx.Node, error) { return yamlx.Seq(), nil }, PrepareMihomoDNS: func(document *yamlx.Node) (*yamlx.Node, error) { return document, nil }}
}
func TestMihomoBaseValidation(t *testing.T) {
	s, _ := serviceFixture(t, "router")
	s.owners.DNS = identityDNS()
	source := "secret: synthetic-controller\nexternal-controller: 127.0.0.1:9090\ndns:\n  nameserver: [https://dns.example.com/dns-query]\nmode: global\ntun:\n  enable: true\n  route-address: [192.0.2.0/24]\n"
	for _, test := range []struct {
		text    string
		secret  *string
		message string
	}{
		{"secret: 123456\ndns: {nameserver: [https://dns.example.com/dns-query]}", nil, msgSecretRequired},
		{source, pointer("123456"), msgTemplateSecret},
		{source, pointer("weak"), msgSecretFormat},
		{source + "mixed-port: 1053\n", nil, "Mihomo"},
	} {
		_, _, err := s.mihomoBase(test.text, false, map[string]string{"1053": " synthetic DNS "}, test.secret)
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("error=%v wanted=%s", err, test.message)
		}
	}
}

func TestMihomoBaseCopiesAliasGraphWithoutExpansion(t *testing.T) {
	owner := identityDNS()
	owner.PrepareMihomoDNS = func(document *yamlx.Node) (*yamlx.Node, error) {
		nameservers := yamlx.Get(yamlx.Get(document, "dns"), "nameserver")
		alias := yamlx.Get(document, "retained-upstreams")
		if alias == nil || alias.Kind != yaml.AliasNode || alias.Alias != nameservers {
			t.Fatal("copy expanded or detached the shared alias graph")
		}
		nameservers.Content = append(nameservers.Content, yamlx.Str("https://other.example.com/dns-query"))
		return document, nil
	}
	s := &Service{owners: ExternalOwners{DNS: owner}}
	source := "secret: synthetic-controller\ndns:\n  nameserver: &upstreams [https://dns.example.com/dns-query]\nretained-upstreams: *upstreams\n"
	text, changes, err := s.mihomoBase(source, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, change := range changes {
		found = found || strings.HasPrefix(change, msgNameservers)
	}
	if !found {
		t.Fatal("DNS mutation changed the original comparison document")
	}
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	retained := yamlx.Resolve(yamlx.Get(node, "retained-upstreams"))
	if retained == nil || len(retained.Content) != 2 {
		t.Fatal("copied graph lost the owner's mutation")
	}
}

func TestMihomoBaseRejectsAmplifyingAndRecursiveAliasesWithinBudget(t *testing.T) {
	var chain strings.Builder
	chain.WriteString("x0: &x0 [leaf]\n")
	for level := 1; level <= 40; level++ {
		fmt.Fprintf(&chain, "x%d: &x%d [*x%d, *x%d]\n", level, level, level-1, level-1)
	}
	for _, test := range []struct{ name, source string }{{"forty-level-amplification", chain.String()}, {"self-reference", "cycle: &cycle [*cycle]\n"}} {
		t.Run(test.name, func(t *testing.T) {
			owner := identityDNS()
			called := false
			owner.PrepareMihomoDNS = func(document *yamlx.Node) (*yamlx.Node, error) { called = true; return document, nil }
			s := &Service{owners: ExternalOwners{DNS: owner}}
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			start := time.Now()
			text, _, err := s.mihomoBase(test.source, false, nil, nil)
			elapsed := time.Since(start)
			runtime.ReadMemStats(&after)
			if err == nil || err.Error() != msgBaseParse || called || text != "" {
				t.Fatal("unsafe YAML reached DNS preparation", err)
			}
			if elapsed > 5*time.Second {
				t.Fatalf("alias rejection exceeded time budget: %s", elapsed)
			}
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 64<<20 {
				t.Fatalf("alias rejection allocated %d bytes", allocated)
			}
		})
	}
}

func TestRouterWizardKeepsOxiDNSPlainUpstreamOutsideMihomoDNS(t *testing.T) {
	s := &Service{owners: ExternalOwners{DNS: identityDNS()}}
	source := "secret: synthetic-controller\ndns: {nameserver: [https://dns.example.com/dns-query]}\ntun: {dns-hijack: ['any:53']}\n"
	text, _, err := s.mihomoBase(source, true, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	hijack := yamlx.Get(yamlx.Get(doc, "tun"), "dns-hijack")
	if hijack == nil || hijack.Kind != yaml.SequenceNode || len(hijack.Content) != 0 {
		t.Fatal("expected explicit empty dns-hijack", hijack)
	}
}
