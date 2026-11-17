package rulefetch

import (
	"bytes"
	"strings"

	"configflow/internal/yamlx"
)

// editRuleYAML uses the shared splice-first editor. A whole-document fallback
// retains the original BOM as well as the facade's comments and line endings.
func editRuleYAML(content string, edit func(*yamlx.Node) error) ([]byte, error) {
	out, err := yamlx.Edit([]byte(content), edit)
	if err != nil {
		return []byte(content), err
	}
	if strings.HasPrefix(content, "\ufeff") && !bytes.HasPrefix(out, []byte("\ufeff")) {
		out = append([]byte("\ufeff"), out...)
	}
	return out, nil
}
