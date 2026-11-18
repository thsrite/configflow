package setup

import (
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func identityDNS() *DNSOwner {
	return &DNSOwner{DefaultLocalDNS: "https://dns.example.com/dns-query", DNSTarget: "127.0.0.1:1053", Nameservers: func() []string { return []string{"https://dns.example.com/dns-query"} }, LocalUpstreams: func(string) (*yamlx.Node, error) { return yamlx.Seq(), nil }, PrepareMihomoDNS: func(document *yamlx.Node) (*yamlx.Node, error) { return document, nil }}
}
func TestMihomoBaseChangesMatchPython(t *testing.T) {
	s, _ := serviceFixture(t, "router")
	s.owners.DNS = identityDNS()
	source := "secret: synthetic-controller\nexternal-controller: 127.0.0.1:9090\ndns:\n  nameserver: [https://dns.example.com/dns-query]\nmode: global\ntun:\n  enable: true\n  route-address: [192.0.2.0/24]\n"
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "gateway", true: "full-tun"}[full], func(t *testing.T) {
			input, _ := json.Marshal(struct {
				Text string `json:"text"`
				Full bool   `json:"full"`
			}{source, full})
			want := pythonSetup(t, `import json,sys,yaml;from backend.common.setup_flow import setup_mihomo_base
x=json.load(sys.stdin);text,changes=setup_mihomo_base(x['text'],full_tun=x['full']);json.dump({'document':yaml.safe_load(text),'changes':changes},sys.stdout,ensure_ascii=False)`, input)
			text, changes, err := s.mihomoBase(source, full, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			node, err := yamlx.Parse([]byte(text))
			if err != nil {
				t.Fatal(err)
			}
			document, err := yamlx.ToJSON(node)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(struct {
				Document json.RawMessage `json:"document"`
				Changes  []string        `json:"changes"`
			}{document, changes})
			equalJSON(t, got, want)
		})
	}
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
