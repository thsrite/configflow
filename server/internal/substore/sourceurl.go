package substore

import (
	"net/url"
	"strings"
)

// SourceURLConfigured reports whether a subscription address can be fetched
// at all: some line of it is an absolute URL with a host. A placeholder such
// as the template subscription's "订阅链接" in a new data directory is not;
// fetching it must neither wake the bundled Sub-Store nor be retried.
func SourceURLConfigured(raw string) bool {
	for _, line := range strings.Split(raw, "\n") {
		u, err := url.Parse(strings.TrimSpace(line))
		if err == nil && u.Scheme != "" && u.Host != "" {
			return true
		}
	}
	return false
}

// errSourceURLUnconfigured is the failure of fetching such an address; its
// InvalidURL class renders as "地址无效" in task status and refresh results.
func errSourceURLUnconfigured() error {
	return &RequestError{Class: "InvalidURL", Message: msgSourceURLUnconfigured}
}
