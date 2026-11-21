//go:build linux

package store

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// fdFlags returns the open flags of every descriptor of this process that
// refers to path.
func fdFlags(t *testing.T, path string) []int64 {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip(err)
	}
	var flags []int64
	for _, entry := range entries {
		link, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err != nil || link != path {
			continue
		}
		info, err := os.ReadFile(filepath.Join("/proc/self/fdinfo", entry.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(info), "\n") {
			if value, found := strings.CutPrefix(line, "flags:"); found {
				flag, err := strconv.ParseInt(strings.TrimSpace(value), 8, 64)
				if err != nil {
					t.Fatal(err)
				}
				flags = append(flags, flag)
			}
		}
	}
	return flags
}

// The idle reader's descriptor now lives for the whole process, so it must
// not leak into child processes (installers, cores): O_CLOEXEC. Control: a
// descriptor opened without it is reported without the flag.
func TestIdleReaderDescriptorIsCloseOnExec(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	head(t, target.Store)
	path := must(filepath.Abs(target.Path()))(t)
	flags := fdFlags(t, path)
	if len(flags) == 0 {
		t.Fatal("no descriptor of the idle reader found")
	}
	for _, flag := range flags {
		if flag&syscall.O_CLOEXEC == 0 {
			t.Fatalf("idle reader descriptor without O_CLOEXEC: flags %o", flag)
		}
	}
	target.Close()
	if left := fdFlags(t, path); len(left) != 0 {
		t.Fatalf("Close left %d descriptors open", len(left))
	}

	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	control := fdFlags(t, path)
	if len(control) != 1 || control[0]&syscall.O_CLOEXEC != 0 {
		t.Fatalf("control: flags %o", control)
	}
}
