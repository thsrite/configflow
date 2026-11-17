package deploy

import (
	"configflow/internal/agents/diag"
	"strings"
)

func hasProtocol(values []int) bool {
	for _, v := range values {
		if v == 1 {
			return true
		}
	}
	return false
}
func safeFailure(err error, token string) string {
	return diag.FailureText(err.Error(), []any{token}, 0)
}
func firstLine(text string, limit int) string {
	line, _, _ := strings.Cut(text, "\n")
	r := []rune(line)
	if len(r) > limit {
		r = r[:limit]
	}
	return string(r)
}
