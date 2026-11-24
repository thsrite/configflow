// Package gaterouting tests server/scripts/gate.sh routing and stage selection
// using bounded shell fakes (no Python, no product tests).
package gaterouting

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeGo = `#!/bin/sh
echo "go $*" >> "$HOME/calls.log"
case "$1" in
  clean) rm -f "$HOME/cached" ;;
  test)
    for arg in "$@"; do
      case "$arg" in
        -race)
          [ -n "$FAIL_RACE" ] && exit 1
          if [ -f "$HOME/cached" ]; then
            echo "ok  configflow/internal/fake  (cached)"
          else
            echo "ok  configflow/internal/fake  1.00s"
            touch "$HOME/cached"
          fi
          exit 0
          ;;
        -fuzz=*)
          [ -n "$FAIL_FUZZ" ] && exit 1
          echo "PASS synthetic fuzz"
          exit 0
          ;;
      esac
    done
    ;;
esac
exit 0
`

const fakeNpm = `#!/bin/sh
echo "npm $*" >> "$HOME/frontend-calls.log"
case "$1" in
  ci) mkdir -p node_modules ;;
  run) [ -n "$FAIL_TYPECHECK" ] && { echo "synthetic type error"; exit 1; } ;;
esac
exit 0
`

const fakeNpx = `#!/bin/sh
echo "npx $*" >> "$HOME/frontend-calls.log"
if [ -n "$FAIL_FRONTEND" ]; then
  echo " FAIL  tests/synthetic.test.ts > fails"
  echo "      Tests  1 failed | 2 passed (3)"
  exit 1
fi
echo "      Tests  3 passed (3)"
`

type gateHarness struct {
	root string
	base string
	env  []string
}

func newGateHarness(t *testing.T) *gateHarness {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("gate.sh uses Linux flock/stat/sha256sum")
	}
	root := t.TempDir()
	scripts := filepath.Join(root, "server", "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "scripts", "gate.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scripts, "gate.sh"), src, 0o755); err != nil {
		t.Fatal(err)
	}
	proddiffStub := "#!/usr/bin/env bash\necho \"proddiff mode=${PRODDIFF_MODE:-unset} base=${PRODDIFF_BASE_TREE:-} comparator=${PRODDIFF_COMPARATOR:-unset} $*\"\n"
	if err := os.WriteFile(filepath.Join(scripts, "proddiff.sh"), []byte(proddiffStub), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"go":    fakeGo,
		"gofmt": "#!/bin/sh\nexit 0\n",
		"npm":   fakeNpm,
		"npx":   fakeNpx,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{
		filepath.Join(root, "rt"),
		filepath.Join(root, "backend", "agents", "go-agent"),
		filepath.Join(root, "frontend"),
		filepath.Join(root, "base", "server"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "frontend", "package.json"), []byte("{\"name\":\"synthetic\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frontend", "package-lock.json"), []byte("{\"lockfileVersion\":3}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	drop := map[string]bool{
		"GATE_ONLY": true, "GATE_FULL": true, "GATE_SLOTS": true,
		"FAIL_RACE": true, "FAIL_FUZZ": true, "FAIL_FRONTEND": true, "FAIL_TYPECHECK": true,
		"GATE_FUZZ_JOBS": true, "GATE_FUZZ_PARALLEL": true, "GATE_FUZZTIME": true,
		"PRODDIFF_MODE": true, "PRODDIFF_BASE_TREE": true, "PRODDIFF_COMPARATOR": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "HOME" || key == "PATH" || key == "GATE_LOGS" || drop[key] {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"HOME="+root,
		"PATH="+bin+":"+os.Getenv("PATH"),
		"GATE_LOGS="+filepath.Join(root, "logs"),
	)
	return &gateHarness{root: root, base: filepath.Join(root, "base"), env: env}
}

func (h *gateHarness) run(t *testing.T, extraEnv map[string]string, args ...string) (int, string) {
	t.Helper()
	cmdArgs := append([]string{filepath.Join(h.root, "server", "scripts", "gate.sh"), "-p", "6"}, args...)
	cmd := exec.Command("bash", cmdArgs...)
	env := append([]string(nil), h.env...)
	for k, v := range extraEnv {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), string(out)
	}
	t.Fatal(err)
	return -1, ""
}

func TestGateCoreFullThenCache(t *testing.T) {
	h := newGateHarness(t)
	code, out := h.run(t, map[string]string{"GATE_ONLY": "core"})
	if code != 0 || !strings.Contains(out, "FULL mode") {
		t.Fatalf("first core: code=%d out=%s", code, out)
	}
	code, out = h.run(t, map[string]string{"GATE_ONLY": "core"})
	if code != 0 || !strings.Contains(out, "CACHE mode") || !strings.Contains(out, "race cache hits=1") {
		t.Fatalf("second core: code=%d out=%s", code, out)
	}
	if strings.Contains(out, "PYREF_INPUT_FINGERPRINT") {
		t.Fatalf("core still sets PYREF_INPUT_FINGERPRINT: %s", out)
	}
}

func TestGateProddiffDefaultsToGo(t *testing.T) {
	h := newGateHarness(t)
	// Missing PRODDIFF_BASE_TREE fails with exit 2 before running proddiff.
	code, out := h.run(t, map[string]string{"GATE_ONLY": "proddiff"})
	if code != 2 || strings.Contains(out, "== proddiff") {
		t.Fatalf("missing base tree: code=%d out=%s", code, out)
	}
	// Python mode and comparator are rejected.
	for _, env := range []map[string]string{
		{"GATE_ONLY": "proddiff", "PRODDIFF_MODE": "python"},
		{"GATE_ONLY": "proddiff", "PRODDIFF_COMPARATOR": "python", "PRODDIFF_BASE_TREE": h.base},
		{"GATE_ONLY": "difftest"},
		{"GATE_ONLY": "pyref"},
	} {
		code, _ := h.run(t, env)
		if code != 2 {
			t.Fatalf("env %v: got exit %d, want 2", env, code)
		}
	}
	code, out = h.run(t, map[string]string{"GATE_ONLY": "proddiff", "PRODDIFF_BASE_TREE": h.base})
	if code != 0 || !strings.Contains(out, "proddiff mode=go base="+h.base+" comparator=go") {
		t.Fatalf("go proddiff: code=%d out=%s", code, out)
	}
}

func TestGateCompareRunsProddiffAndFrontend(t *testing.T) {
	h := newGateHarness(t)
	code, out := h.run(t, map[string]string{"GATE_ONLY": "compare", "PRODDIFF_BASE_TREE": h.base})
	if code != 0 || !strings.Contains(out, "proddiff mode=go") || !strings.Contains(out, "gate: frontend PASS") {
		t.Fatalf("compare: code=%d out=%s", code, out)
	}
	code, out = h.run(t, map[string]string{"GATE_ONLY": "compare", "PRODDIFF_BASE_TREE": h.base, "FAIL_FRONTEND": "1"})
	if code == 0 || !strings.Contains(out, "GATE FAIL: frontend (vitest") || !strings.Contains(out, "GATE FAIL: compare") {
		t.Fatalf("failed frontend in compare: code=%d out=%s", code, out)
	}
}
