package agents

import (
	"net/url"
	"os"

	"configflow/internal/weburl"
)

// environ is os.environ (a variable for tests).
var environ = os.Environ

// environProxy is the proxy of a manager call from the environment
// (weburl.FromEnvironment: no automatic loopback bypass, ALL_PROXY counts):
// nil when the request goes direct.
func environProxy(target *url.URL) (*url.URL, error) {
	return weburl.FromEnvironment(environ(), target)
}
