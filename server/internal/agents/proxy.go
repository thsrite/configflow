package agents

// The proxy choice of requests 2.31 for a Session with trust_env=True (every
// manager call; monitor/panel requests set NoProxy) lives in the shared
// package internal/reqproxy (also used by the Sub-Store client); this file
// only binds it to os.environ. Reference: testdata/gen_proxy_reference.py
// (real requests 2.31 / urllib3 2.8 on CPython 3.11), asserted here by
// TestReferenceEnvironmentProxies.

import (
	"os"

	"configflow/internal/reqproxy"
)

// environ is os.environ (a variable for tests).
var environ = os.Environ

// envProxy is the proxy requests would use.
type envProxy = reqproxy.Proxy

// requests' InvalidURL, InvalidProxyURL and InvalidSchema for a proxy value.
var (
	errInvalidURL      = reqproxy.ErrInvalidURL
	errInvalidProxyURL = reqproxy.ErrInvalidProxyURL
	errInvalidSchema   = reqproxy.ErrInvalidSchema
)

// environProxy is get_environ_proxies + get_connection for a prepared URL:
// nil when the request goes direct.
func environProxy(prepared string) (*envProxy, error) {
	return reqproxy.FromEnvironment(environ(), prepared)
}
