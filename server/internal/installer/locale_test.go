package installer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunShellLocaleDoesNotDependOnCaller(t *testing.T) {
	for _, locale := range []string{"unset", "C", "C.UTF-8"} {
		t.Run(locale, func(t *testing.T) {
			for _, key := range []string{"LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES"} {
				t.Setenv(key, locale)
				if locale == "unset" {
					if err := os.Unsetenv(key); err != nil {
						t.Fatal(err)
					}
				}
			}
			for _, shell := range shells() {
				for _, replace := range []bool{false, true} {
					mode := "inherit"
					if replace {
						mode = "replace"
					}
					t.Run(filepath.Base(shell)+"/"+mode, func(t *testing.T) {
						script := writeFile(t, filepath.Join(t.TempDir(), "locale.sh"), `set -eu
text="$1"
printf '%s\n' "$LANG" "$LC_ALL" "$text"
`, 0o644)
						// Explicit options can also contain a stale caller locale.
						r := runShell(t, shell, script, runOptions{args: []string{"安装与维护"},
							env: []string{"PATH=" + os.Getenv("PATH"), "LANG=C", "LC_ALL=C"}, replace: replace})
						if want := "C.UTF-8\nC.UTF-8\n安装与维护\n"; r.code != 0 || r.stdout != want || r.stderr != "" {
							t.Fatalf("exit %d, stdout %q, stderr %q; want %q", r.code, r.stdout, r.stderr, want)
						}
					})
				}
			}
		})
	}
}
