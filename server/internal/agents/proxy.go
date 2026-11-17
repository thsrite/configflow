package agents

import (
	"net/url"

	"configflow/internal/weburl"
)

// environProxy selects a manager call's proxy using Go's native rules.
func environProxy(target *url.URL) (*url.URL, error) {
	return weburl.FromEnvironment(target)
}
