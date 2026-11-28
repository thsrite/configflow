package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

// uncachedAgentChecksums is the original installer hashing loop.
func uncachedAgentChecksums() (map[string]string, error) {
	directory := AgentsStaticDir()
	if value, set := os.LookupEnv("AGENTS_STATIC_DIR"); set && value == "" {
		directory = "."
	}
	checksums := map[string]string{}
	for _, arch := range []string{"linux-amd64", "linux-arm64"} {
		path := filepath.Join(directory, "configflow-agent-"+arch)
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, file)
		file.Close()
		if err != nil {
			return nil, err
		}
		checksums[arch] = hex.EncodeToString(hash.Sum(nil))
	}
	return checksums, nil
}

func BenchmarkLocalAgentChecksums(b *testing.B) {
	dir := b.TempDir()
	b.Setenv("AGENTS_STATIC_DIR", dir)
	content := bytes.Repeat([]byte{'x'}, 10<<20)
	copy(content, "\x7fELF")
	for _, arch := range []string{"linux-amd64", "linux-arm64"} {
		if err := os.WriteFile(filepath.Join(dir, "configflow-agent-"+arch), content, 0600); err != nil {
			b.Fatal(err)
		}
	}
	want, err := uncachedAgentChecksums()
	if err != nil {
		b.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		checksums func() (map[string]string, error)
	}{{"before", uncachedAgentChecksums}, {"after", LocalAgentChecksums}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			if got, err := tc.checksums(); err != nil || !maps.Equal(got, want) {
				b.Fatal(got, err)
			}
			for b.Loop() {
				if _, err := tc.checksums(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
