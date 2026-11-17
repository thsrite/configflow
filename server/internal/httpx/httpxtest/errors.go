package httpxtest

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
)

// MigratedAPIError verifies the approved text/HTML-to-JSON error migration. It accepts
// only the same error status and exact public description, with success:false
// and no extra JSON fields. Callers must still compare all unrelated headers and
// stored effects; this is not a general error-response skip.
func MigratedAPIError(method string, pyStatus, goStatus int, pyHeader, goHeader http.Header, pyBody, goBody []byte) bool {
	if pyStatus < 400 || pyStatus != goStatus || !strings.HasPrefix(goHeader.Get("Content-Type"), "application/json") {
		return false
	}
	if !(strings.HasPrefix(pyHeader.Get("Content-Type"), "text/html") || pyStatus < 500 && strings.HasPrefix(pyHeader.Get("Content-Type"), "text/plain")) {
		return false
	}
	if method == "HEAD" {
		return len(pyBody) == 0 && len(goBody) == 0
	}
	var expected string
	switch {
	case strings.HasPrefix(pyHeader.Get("Content-Type"), "text/plain") && pyStatus < 500:
		expected = strings.TrimSuffix(string(pyBody), "\n")
	case strings.HasPrefix(pyHeader.Get("Content-Type"), "text/html"):
		_, paragraph, ok := strings.Cut(string(pyBody), "<p>")
		if !ok {
			return false
		}
		paragraph, _, ok = strings.Cut(paragraph, "</p>")
		if !ok {
			return false
		}
		expected = html.UnescapeString(strings.ReplaceAll(paragraph, "<br>", "\n"))
	default:
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(goBody, &fields) != nil || len(fields) != 2 || string(fields["success"]) != "false" {
		return false
	}
	var message string
	return json.Unmarshal(fields["message"], &message) == nil && message == expected
}
