package yamlxtest

import (
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Check classifies every occurrence in a caller's real test inputs. It logs
// counts only; an unclassified row fails with its index, without source secrets.
func Check(t testing.TB, caller string, sources []string) map[string]int {
	t.Helper()
	counts := map[string]int{}
	grammar := map[string]int{}
	for i, source := range sources {
		class, err := Classify(source)
		if err != nil {
			counts["unclassified"]++
			t.Errorf("%s input %d (%d bytes): %v", caller, i, len(source), err)
			continue
		}
		counts[class]++
		if class == SyntaxRange {
			switch {
			case strings.Contains(source, "\t"):
				grammar["TAB"]++
			case strings.HasPrefix(source, "\ufeff\ufeff"):
				grammar["C"]++
			case strings.Contains(source, "&x 2"):
				grammar["B"]++
			default:
				grammar["A"]++
			}
		}
	}
	t.Logf("N7-3 migration caller=%s rows=%d classes=%v", caller, len(sources), counts)
	if len(grammar) != 0 {
		t.Logf("N7-3 grammar caller=%s subclasses=%v", caller, grammar)
	}
	return counts
}

// TextFields extracts selected YAML text fields from stored JSON fixtures.
// It follows JSON containers only; no parsed YAML graph is traversed here.
func TextFields(value any, names ...string) []string {
	wanted := map[string]bool{}
	for _, name := range names {
		wanted[name] = true
	}
	var result []string
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case *jsonx.Object:
			for key, item := range value.All() {
				if wanted[key] {
					if text, ok := item.(string); ok {
						result = append(result, text)
					}
				}
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
