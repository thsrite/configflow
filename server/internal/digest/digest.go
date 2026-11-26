// Package digest computes lowercase hexadecimal cryptographic digests. It does
// not implement streaming hashers or canonical JSON encoding.
package digest

import (
	"crypto/sha256"
	"encoding/hex"
)

// SHA256Hex returns the lowercase hexadecimal SHA-256 digest of data.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SHA256HexString returns the lowercase hexadecimal SHA-256 digest of s.
func SHA256HexString(s string) string {
	return SHA256Hex([]byte(s))
}
