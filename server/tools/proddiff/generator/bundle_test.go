package main

import (
	"configflow/internal/deploy"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestBundleMetadataChecksRemainStrictForStructuredProviders(t *testing.T) {
	data := []byte("proxies: []\n")
	sum := sha256.Sum256(data)
	file := deploy.File{Path: "providers/a.yaml", Role: deploy.RoleProvider, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
	if err := verifyBundleFile(file, data); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"bytes", "size", "hash"} {
		t.Run(change, func(t *testing.T) {
			bad, body := file, append([]byte(nil), data...)
			switch change {
			case "bytes":
				body[0] = 'X'
			case "size":
				bad.Size++
			case "hash":
				bad.SHA256 = "wrong"
			}
			if verifyBundleFile(bad, body) == nil {
				t.Fatal("corrupt manifest/file accepted")
			}
		})
	}
}
