package yamlx

import (
	"crypto/sha256"
	"fmt"
	"testing"

	"gopkg.in/yaml.v3"
)

func largeMarshalDocument() *Node {
	proxies := make([]*Node, 320)
	for i := range proxies {
		protocols := Seq(Str("h2"), Str("http/1.1"))
		protocols.Style = yaml.FlowStyle
		proxies[i] = Map(
			Str("name"), Str(fmt.Sprintf("测试节点 %03d", i)),
			Str("type"), Str("ss"),
			Str("server"), Str(fmt.Sprintf("proxy-%03d.example.com", i)),
			Str("port"), Int(8000+int64(i)),
			Str("udp"), Bool(i%2 == 0),
			Str("options"), Map(Str("protocols"), protocols, Str("note"), Str("first line\nsecond line\n")),
		)
	}
	proxies[0].HeadComment = "Synthetic provider"
	return Map(Str("proxies"), Seq(proxies...))
}

func TestMarshalLargeDocumentBytes(t *testing.T) {
	data, err := MarshalChecked(t, largeMarshalDocument())
	if err != nil {
		t.Fatal(err)
	}
	// Captured from unmodified yaml.v3 v3.0.1.
	const want = "3b45a209b1d5c5e25317cc578d0a2cf3f1f34b814e3664840ced901cc350a968"
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != want {
		t.Fatalf("SHA-256 of %d output bytes = %s, want %s", len(data), got, want)
	}
}

func BenchmarkMarshalLargeDocument(b *testing.B) {
	document := largeMarshalDocument()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Marshal(document); err != nil {
			b.Fatal(err)
		}
	}
}
