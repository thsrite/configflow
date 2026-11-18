package setup

import (
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/yamlx"
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
