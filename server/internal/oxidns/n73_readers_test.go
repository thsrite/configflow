package oxidns

import (
	"bufio"
	"compress/gzip"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamltest"
)

func n73ReaderDiagnostic(err error) bool {
	var read *yamlReadError
	if !errors.As(err, &read) {
		return false
	}
	return errors.Is(read.cause, yamlx.ErrPolicy) || errors.Is(read.cause, yamlx.ErrAliasRecursion) || errors.Is(read.cause, yamlx.ErrAliasBudget)
}

func TestN73OxidnsReaderInputs(t *testing.T) {
	paths, err := filepath.Glob(testdataDir + "*.jsonl.gz")
	if err != nil || len(paths) < 10 {
		t.Fatal("reader fixtures missing")
	}
	totals := map[string]map[string]int{"native": {}, "template": {}}
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 65536), 1<<28)
		rowIndex := 0
		for scanner.Scan() {
			rowIndex++
			v, err := jsonx.Parse(scanner.Bytes())
			row, ok := v.(*jsonx.Object)
			if err != nil || !ok {
				t.Fatal("fixture JSON failed")
			}
			args := row.Value("args")
			native := yamltest.TextFields(args, "native_config")
			native = append(native, n73IncludeInputs(args)...)
			if row.Value("fn") == "load_native_yaml" {
				if list, ok := args.([]any); ok && len(list) != 0 {
					if text, ok := list[0].(string); ok {
						native = append(native, text)
					}
				}
			}
			template := yamltest.TextFields(args, "custom_config", "local_dns", "remote_dns", "fallback_dns")
			for caller, inputs := range map[string][]string{"native": native, "template": template} {
				for _, source := range inputs {
					class, err := n73ReaderClass(caller, source)
					if err != nil {
						totals[caller]["unknown"]++
						t.Errorf("%s row %d reader %s (%d bytes): %v", filepath.Base(path), rowIndex, caller, len(source), err)
					} else {
						totals[caller][class]++
					}
				}
			}
		}
		if scanner.Err() != nil {
			t.Fatal(scanner.Err())
		}
		reader.Close()
		file.Close()
	}
	for caller, counts := range totals {
		if len(counts) == 0 {
			t.Fatal("empty reader coverage")
		}
		t.Logf("N7-3d oxidns reader=%s classes=%v unknown=%d", caller, counts, counts["unknown"])
	}
}

func n73ReaderClass(caller, source string) (string, error) {
	var before, after *Node
	var oldErr, newErr error
	oldOK, newOK := true, true
	if caller == "native" {
		syntax := errors.New("test syntax failure")
		before, oldErr = legacyParseYAML(source, syntax)
		after, newErr = parseYAML(source, syntax)
	} else {
		before, oldOK, oldErr = legacySafeLoad(source)
		after, newOK, newErr = safeLoad(source)
	}
	if oldOK == newOK && ((oldErr == nil && newErr == nil && yamlx.Equal(before, after)) ||
		(oldErr != nil && newErr != nil && oldErr.Error() == newErr.Error())) {
		return yamltest.Equal, nil
	}
	if errors.Is(newErr, yamlx.ErrAliasRecursion) || errors.Is(newErr, yamlx.ErrAliasBudget) {
		return yamltest.Alias, nil
	}
	if errors.Is(newErr, yamlx.ErrDuplicateKey) {
		return yamltest.Duplicate, nil
	}
	if errors.Is(newErr, yamlx.ErrPolicy) {
		return yamltest.Policy, nil
	}
	// Repeated anchors are accepted by the native parser (5.7 grammar B).
	if root, err := yamlx.Parse([]byte(source)); err == nil && duplicateAnchor(root, map[string]bool{}) {
		return yamltest.SyntaxRange, nil
	}
	class, err := yamltest.Classify(source)
	if err != nil {
		return "", err
	}
	if class == yamltest.Equal {
		return "", fmt.Errorf("reader delta outside approved categories")
	}
	return class, nil
}

func TestN73OxidnsReaderSafety(t *testing.T) {
	var amplified strings.Builder
	amplified.WriteString("a0: &a0 [leaf]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&amplified, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, test := range []struct {
		name, source string
		cause        error
	}{
		{"depth-40-alias", amplified.String(), yamlx.ErrAliasBudget},
		{"recursive", "x: &x [*x]\n", yamlx.ErrAliasRecursion},
		{"duplicate", "plugins: []\nplugins: []\n", yamlx.ErrDuplicateKey},
		{"merge-source", "<<: 5\n", yamlx.ErrPolicy},
		{"tag-shape", "x: !!seq scalar\n", yamlx.ErrPolicy},
	} {
		t.Run(test.name, func(t *testing.T) {
			syntax := errors.New("syntax-only contract")
			if _, err := parseYAML(test.source, syntax); !errors.Is(err, test.cause) || errors.Is(err, syntax) {
				t.Fatalf("native cause lost: %v", err)
			}
			if _, ok, err := safeLoad(test.source); ok || !errors.Is(err, test.cause) {
				t.Fatalf("policy became template fallback: %v", err)
			}
		})
	}
	syntax := errors.New("syntax-only contract")
	if _, err := parseYAML("plugins: [", syntax); !errors.Is(err, syntax) {
		t.Fatal("native syntax contract changed")
	}
	if _, ok, err := safeLoad("plugins: ["); ok || err != nil {
		t.Fatal("ordinary template syntax did not retain fallback")
	}
}

func TestN73OxidnsNativeBusinessBuilderPreserved(t *testing.T) {
	for _, source := range []string{
		"shared: &s [one, two]\ncopy: *s\nplugins: []\n",
		"defaults: &d {a: 1, b: two}\ncopy: {<<: *d, b: three}\nplugins: []\n",
	} {
		syntax := errors.New("syntax-only contract")
		before, oldErr := legacyParseYAML(source, syntax)
		after, err := parseYAML(source, syntax)
		if oldErr != nil || err != nil || !yamlx.Equal(before, after) {
			t.Fatal("native builder or merge validation changed")
		}
	}
}

// Include YAML files are separate reader inputs, not JSON settings text fields.
func n73IncludeInputs(value any) []string {
	result := []string{}
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case *jsonx.Object:
			if files, ok := value.Value("additional_files").([]any); ok {
				for _, item := range files {
					file, ok := item.(*jsonx.Object)
					if !ok {
						continue
					}
					path := file.GetString("path")
					if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
						continue
					}
					content, ok := file.Value("content").(string)
					if !ok {
						continue
					}
					if file.GetString("encoding") == "base64" {
						data, err := base64.StdEncoding.DecodeString(content)
						if err != nil || !utf8.Valid(data) {
							continue
						}
						content = string(data)
					}
					if len(content) <= MaxNativeBytes {
						result = append(result, content)
					}
				}
			}
			for _, item := range value.All() {
				visit(item)
			}
		case []any:
			for _, item := range value {
				visit(item)
			}
		}
	}
	visit(value)
	return result
}
