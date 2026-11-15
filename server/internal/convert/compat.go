package convert

import "strings"

// hasYAMLPayload is ruleset_content._has_yaml_payload:
// re.search(r"^\s*(?:payload|\"payload\"|'payload')\s*:", probe, re.M) or
// probe.lstrip().startswith('{').
// TODO(port): replace with C14 (backend/utils/ruleset_content.py).
func hasYAMLPayload(content string) bool {
	probe := strings.TrimPrefix(content, "\ufeff")
	skipSpace := func(s string) string { return strings.TrimLeftFunc(s, pyIsSpace) }
	for start := 0; start <= len(probe); {
		rest := skipSpace(probe[start:])
		for _, key := range []string{`payload`, `"payload"`, `'payload'`} {
			if strings.HasPrefix(rest, key) && strings.HasPrefix(skipSpace(rest[len(key):]), ":") {
				return true
			}
		}
		next := strings.IndexByte(probe[start:], '\n')
		if next < 0 {
			break
		}
		start += next + 1
	}
	return strings.HasPrefix(skipSpace(probe), "{")
}
