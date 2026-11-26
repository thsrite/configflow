package dnsfollow

import (
	"fmt"
	"os"
	"testing"

	"configflow/internal/lockx"
)

// TestMain enforces design 3.4: every round, binding change and finalization
// takes its locks in O3 order, so lockx records no violation for the package.
func TestMain(m *testing.M) {
	code := m.Run()
	if n := lockx.Violations(); n != 0 {
		fmt.Fprintf(os.Stderr, "lockx: %d lock order violations\n", n)
		code = 1
	}
	os.Exit(code)
}
