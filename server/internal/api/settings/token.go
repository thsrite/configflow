package settings

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const tokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// randomToken returns length letters and digits, each drawn uniformly from
// the system CSPRNG.
func randomToken(length int) (string, error) {
	out := make([]byte, length)
	limit := big.NewInt(int64(len(tokenAlphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate token: %w", err)
		}
		out[i] = tokenAlphabet[n.Int64()]
	}
	return string(out), nil
}
