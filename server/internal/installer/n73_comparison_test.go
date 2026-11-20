package installer

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"testing"
)

func testComposeArchive(t *testing.T, compose, script string, mode uint32, extra bool) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range [][2]string{{"compose.yaml", compose}, {"install.sh", script}} {
		header := &zip.FileHeader{Name: entry[0], Method: zip.Deflate, ExternalAttrs: mode << 16}
		stream, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Write([]byte(entry[1])); err != nil {
			t.Fatal(err)
		}
	}
	if extra {
		stream, err := writer.Create("unexpected")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = stream.Write([]byte("extra")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestN73SemanticComparisonRejectsMeaningfulChanges(t *testing.T) {
	reference := map[string]any{"content_base64": testComposeArchive(t, "services: {agent: {image: synthetic, replicas: 1}}\n", "echo synthetic\n", 0o100600, false)}
	tests := []struct {
		name, compose, script string
		mode                  uint32
		extra, want           bool
	}{
		{"layout", "services:\n  agent:\n    image: 'synthetic'\n    replicas: 1\n", "echo synthetic\n", 0o100600, false, true},
		{"value", "services: {agent: {image: changed, replicas: 1}}\n", "echo synthetic\n", 0o100600, false, false},
		{"type", "services: {agent: {image: synthetic, replicas: '1'}}\n", "echo synthetic\n", 0o100600, false, false},
		{"shell bytes", "services: {agent: {image: synthetic, replicas: 1}}\n", "echo changed\n", 0o100600, false, false},
		{"permission", "services: {agent: {image: synthetic, replicas: 1}}\n", "echo synthetic\n", 0o100777, false, false},
		{"extra member", "services: {agent: {image: synthetic, replicas: 1}}\n", "echo synthetic\n", 0o100600, true, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := map[string]any{"content_base64": testComposeArchive(t, test.compose, test.script, test.mode, test.extra)}
			if got := nativeOutcomeEqual(reference, actual, ""); got != test.want {
				t.Fatalf("equal=%v want %v", got, test.want)
			}
		})
	}
}
