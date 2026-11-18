package subs

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
)

func nativeDNSExpectations(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_dns_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SourceSHA string `json:"source_sha256"`
		Cases     map[string]struct {
			InputSHA string `json:"input_sha256"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/dns.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	if hex.EncodeToString(sum[:]) != manifest.SourceSHA {
		t.Fatal("DNS reference source drift")
	}
	z, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	scanner := bufio.NewScanner(z)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for key, entry := range manifest.Cases {
		index, err := strconv.Atoi(key)
		if err != nil || index < 0 || index >= len(lines) {
			t.Fatal("DNS fixture index", key)
		}
		sum := sha256.Sum256([]byte(lines[index]))
		if hex.EncodeToString(sum[:]) != entry.InputSHA {
			t.Fatalf("DNS input drift %s", key)
		}
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object).GetObject("cases")
}

func TestN89DNSOwnsOnlyGeneratedSubtree(t *testing.T) {
	main, err := yamlx.Parse([]byte("hosts: {source.example: target.example}\ndns: &dns\n  enable: true\n  listen: '127.0.0.1:53'\n  proxy-server-nameserver: ['https://dns.example/dns-query']\n  enhanced-mode: fake-ip\n  fake-ip-filter: &filter [kept.example]\nbackup: *dns\nfilter-backup: *filter\n"))
	if err != nil {
		t.Fatal(err)
	}
	old := yamlx.Clone(yamlx.Get(main, "backup"))
	merged := yamlx.Map(yamlx.Str("source.example"), yamlx.Str("target.example"))
	report, err := ApplyDNS(main, merged, "v1.19.20")
	if err != nil || report == nil || !report.Applied {
		t.Fatalf("apply %#v %v", report, err)
	}
	if !yamlx.Equal(old, yamlx.Get(main, "backup")) {
		t.Fatal("unowned alias changed")
	}
	if !yamlx.Equal(yamlx.Get(main, "filter-backup"), yamlx.Seq(yamlx.Str("kept.example"))) {
		t.Fatal("unowned filter alias changed")
	}
	wire, err := yamlx.Marshal(main)
	if err != nil {
		t.Fatal(err)
	}
	reparsed, err := yamlx.Parse(wire)
	if err != nil || !yamlx.Equal(main, reparsed) {
		t.Fatalf("invalid generated graph: %v", err)
	}
	policy := yamlx.Get(yamlx.Get(main, "dns"), PolicyKey)
	if !yamlx.Equal(yamlx.Get(policy, "source.example"), yamlx.Str("udp://127.0.0.1:53")) {
		t.Fatal("missing generated policy")
	}
}

func TestN89DNSMalformedFilterIsUntouched(t *testing.T) {
	for _, source := range []string{"false", "0", "\"\"", "{}"} {
		t.Run(source, func(t *testing.T) {
			main, err := yamlx.FromJSON([]byte(`{"hosts":{"source.example":"target.example"},"dns":{"enable":true,"listen":"127.0.0.1:53","proxy-server-nameserver":["https://dns.example/dns-query"],"enhanced-mode":"fake-ip","fake-ip-filter-mode":"rule","fake-ip-filter":` + source + `}}`))
			if err != nil {
				t.Fatal(err)
			}
			before, _ := yamlx.ToJSON(main)
			report, err := ApplyDNS(main, yamlx.Map(yamlx.Str("source.example"), yamlx.Str("target.example")), "v1.19.20")
			if err != nil || report.Applied || len(report.Warnings) != 1 || report.Warnings[0] != Warnings["filter_not_list"] {
				t.Fatalf("report %#v %v", report, err)
			}
			after, _ := yamlx.ToJSON(main)
			if !bytes.Equal(before, after) {
				t.Fatal("malformed filter was rewritten")
			}
		})
	}
}
func dnsAllocationFixture(n int) (*yamlx.Node, *yamlx.Node) {
	hosts, merged := yamlx.Map(), yamlx.Map()
	for i := 0; i < n; i++ {
		domain := fmt.Sprintf("node-%d.example", i)
		target := fmt.Sprintf("target-%d.invalid", i)
		yamlx.Set(hosts, domain, yamlx.Str(target))
		yamlx.Set(merged, domain, yamlx.Str(target))
	}
	return yamlx.Map(yamlx.Str("hosts"), hosts, yamlx.Str("dns"), yamlx.Map(yamlx.Str("enable"), yamlx.Bool(false))), merged
}
func TestN89DNSAllocationGrowth(t *testing.T) {
	measure := func(n int) float64 {
		main, merged := dnsAllocationFixture(n)
		return testing.AllocsPerRun(5, func() {
			report, err := ApplyDNS(main, merged, "")
			if err != nil || report.Mapped != n {
				panic("invalid benchmark fixture")
			}
		})
	}
	small, large := measure(64), measure(256)
	if large > small*6 {
		t.Fatalf("allocation growth indicates per-node reindex/decode: 64=%v 256=%v", small, large)
	}
}
func BenchmarkN89DNSHostIndex(b *testing.B) {
	for _, size := range []int{32, 256, 2048} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			main, merged := dnsAllocationFixture(size)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				report, err := ApplyDNS(main, merged, "")
				if err != nil || report.Mapped != size {
					b.Fatal("invalid benchmark", err)
				}
			}
		})
	}
}
