package main

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// O9 round 2 L4: go-agent's go.mod must declare toolchain >= go1.26.11 so local
// and CI builds include the GO-2026-6859 net/http fix.
func TestO9R2ToolchainVersion(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	var got string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "toolchain "); ok {
			got = strings.TrimSpace(after)
			break
		}
	}
	if got == "" {
		t.Fatal("go.mod does not declare a toolchain directive")
	}
	if !toolchainAtLeast(got, "go1.26.11") {
		t.Fatalf("go.mod toolchain = %q, want >= go1.26.11", got)
	}

	// Negative control: verify the comparator rejects 1.26.10 and accepts 1.26.11 / 1.27.4.
	checks := []struct {
		version string
		want    bool
	}{
		{"go1.26.10", false},
		{"go1.25.99", false},
		{"go1.26.11", true},
		{"go1.26.12", true},
		{"go1.27.0", true},
		{"go1.27.4", true},
	}
	for _, tc := range checks {
		if ok := toolchainAtLeast(tc.version, "go1.26.11"); ok != tc.want {
			t.Errorf("toolchainAtLeast(%q, go1.26.11) = %v, want %v", tc.version, ok, tc.want)
		}
	}
}

func toolchainAtLeast(version, minimum string) bool {
	parse := func(v string) ([3]int, bool) {
		v = strings.TrimPrefix(strings.TrimSpace(v), "go")
		parts := strings.Split(v, ".")
		if len(parts) < 2 || len(parts) > 3 {
			return [3]int{}, false
		}
		var out [3]int
		for i, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil || n < 0 {
				return [3]int{}, false
			}
			out[i] = n
		}
		return out, true
	}
	a, okA := parse(version)
	b, okB := parse(minimum)
	if !okA || !okB {
		return false
	}
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}
