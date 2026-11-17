package routersplit

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamltest"
	"gopkg.in/yaml.v3"
)

type nativeOracle struct {
	input       io.WriteCloser
	output      *bufio.Scanner
	command     *exec.Cmd
	diagnostics bytes.Buffer
}

func newNativeOracle(t *testing.T) *nativeOracle {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	python := filepath.Join(home, "cf-test/.venv/bin/python")
	if selected := os.Getenv("CONFIGFLOW_DIFF_PYTHON"); selected != "" {
		python = selected
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	command := exec.CommandContext(ctx, python, "-u", "server/internal/routersplit/testdata/n73_reference.py")
	command.Dir = root
	command.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+t.TempDir(), "PYTHONDONTWRITEBYTECODE=1")
	oracle := &nativeOracle{command: command}
	command.Stderr = &oracle.diagnostics
	oracle.input, err = command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	oracle.output = bufio.NewScanner(output)
	oracle.output.Buffer(make([]byte, 1<<16), 1<<24)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = oracle.input.Close(); _ = command.Wait(); cancel() })
	return oracle
}

func rowYAML(row recordedRow) (string, bool) {
	switch row.Fn {
	case "full_tun_base_yaml":
		value, ok := row.Args.Value("text").(string)
		return value, ok
	case "base_is_full_tun", "check_generation", "mode_from_context", "router_split_state":
		config, _ := row.Args.Value("config").(*jsonx.Object)
		mihomo, _ := config.Value("mihomo").(*jsonx.Object)
		value, ok := mihomo.Value("custom_config").(string)
		return value, ok
	}
	return "", false
}

func (o *nativeOracle) compare(t *testing.T, row recordedRow, output any, actualErr error) (string, bool) {
	t.Helper()
	source, ok := rowYAML(row)
	if !ok {
		return "", false
	}
	class, err := yamltest.Classify(source)
	if err != nil {
		t.Logf("N7-3 unclassified %s %.80q: %v", row.Fn, source, err)
		return "", false
	}
	if class == yamltest.Equal || class == yamltest.Diagnostics {
		return "", false
	}
	normalized, parseErr := yamltest.ReferenceText(source)
	if parseErr != nil {
		normalized = "["
	} // Python's existing invalid-document path.
	args := row.Args.DeepCopy()
	if row.Fn == "full_tun_base_yaml" {
		args.Set("text", normalized)
	} else {
		args.GetObject("config").GetObject("mihomo").Set("custom_config", normalized)
	}
	data, err := jsonx.Compact(jsonx.Obj("fn", row.Fn, "args", args))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(o.input, string(data)); err != nil {
		t.Fatal(err)
	}
	if !o.output.Scan() {
		t.Fatalf("native-policy Python oracle: %v %s", o.output.Err(), o.diagnostics.String())
	}
	value, err := jsonx.Parse(o.output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	answer := value.(*jsonx.Object)
	if expected := answer.GetObject("err"); expected != nil {
		return class, compareError(expected, actualErr) == ""
	}
	if actualErr != nil {
		return class, false
	}
	expected := answer.Value("out")
	if row.Fn == "full_tun_base_yaml" {
		got, gok := output.(string)
		want, wok := expected.(string)
		return class, gok && wok && yamltest.SemanticEqual(got, want)
	}
	return class, jsonx.Equal(expected, output)
}

// nativeFullTunInvariant covers native non-string-key maps where Python folds
// bool/int keys together. It verifies every source pair outside tun unchanged.
func nativeFullTunInvariant(source, output string) bool {
	input, err := yamlx.Parse([]byte(source))
	if err != nil {
		return false
	}
	result, err := yamlx.Parse([]byte(output))
	if err != nil {
		return false
	}
	for key, value := range yamlx.AllPairs(input) {
		key = yamlx.Resolve(key)
		if key.Kind == yaml.ScalarNode && key.ShortTag() == "!!str" && key.Value == "tun" {
			continue
		}
		found := false
		for resultKey, resultValue := range yamlx.AllPairs(result) {
			if yamlx.Equal(key, resultKey) && yamlx.Equal(value, resultValue) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if !fullTunNodeShape(result) || len(nodeTunFilters(yamlx.Get(result, "tun"))) != 0 {
		return false
	}
	rootCount := 0
	for range yamlx.AllPairs(input) {
		rootCount++
	}
	if yamlx.Get(input, "tun") == nil {
		rootCount++
	}
	resultCount := 0
	for range yamlx.AllPairs(result) {
		resultCount++
	}
	if resultCount != rootCount {
		return false
	}
	originalTun := yamlx.Resolve(yamlx.Get(input, "tun"))
	resultTun := yamlx.Resolve(yamlx.Get(result, "tun"))
	forced := map[string]bool{"enable": true, "auto-route": true, "auto-redirect": true, "auto-detect-interface": true, "dns-hijack": true}
	expectedFields := map[string]bool{"enable": true, "auto-route": true, "auto-redirect": true, "auto-detect-interface": true, "dns-hijack": true, "stack": true}
	preservedComplex := 0
	for key, value := range yamlx.AllPairs(originalTun) {
		resolved := yamlx.Resolve(key)
		if resolved.Kind != yaml.ScalarNode || resolved.ShortTag() != "!!str" {
			preservedComplex++
			continue
		}
		removed := false
		for _, name := range RouteKeysRemoved {
			removed = removed || resolved.Value == name
		}
		if removed {
			continue
		}
		expectedFields[resolved.Value] = true
		if !forced[resolved.Value] && !yamlx.Equal(value, yamlx.Get(resultTun, resolved.Value)) {
			return false
		}
	}
	tunCount := 0
	for range yamlx.AllPairs(resultTun) {
		tunCount++
	}
	return tunCount == len(expectedFields)+preservedComplex
}
