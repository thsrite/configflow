package installer

import (
	"strings"

	"configflow/internal/urlx"
)

// pyURLSplit is urllib.parse.urlsplit(url) (CPython 3.11, shared urlx port);
// the error is the ValueError urlsplit raises.
func pyURLSplit(url string) (urlx.SplitResult, error) {
	parsed, err := urlx.Split(url)
	if err != nil {
		return parsed, valueError(err.Error())
	}
	return parsed, nil
}

// hasCredentials reports whether SplitResult.username or .password is truthy.
func hasCredentials(r urlx.SplitResult) bool {
	i := strings.LastIndexByte(r.Netloc, '@')
	if i < 0 {
		return false
	}
	username, password, _ := strings.Cut(r.Netloc[:i], ":")
	return username != "" || password != ""
}
