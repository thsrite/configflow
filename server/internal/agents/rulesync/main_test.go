package rulesync

import (
	"fmt"
	"os"
	"testing"

	"configflow/internal/lockx"
)

// TestMain enforces design 3.4: the worker key, deployment preparation keys and
// repository transactions are always taken in O3 order (no lockx violation).
func TestMain(m *testing.M) {
	code := m.Run()
	if n := lockx.Violations(); n != 0 {
		fmt.Fprintf(os.Stderr, "lockx: %d lock order violations\n", n)
		code = 1
	}
	os.Exit(code)
}
