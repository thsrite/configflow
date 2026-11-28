//go:build linux

package pagecache

import "testing"

// SetFadviseForTest observes advice on the original, still-open descriptor.
func SetFadviseForTest(t *testing.T, fn func(int, int64, int64, int) error) {
	t.Helper()
	previous := fadvise
	fadvise = fn
	t.Cleanup(func() { fadvise = previous })
}
