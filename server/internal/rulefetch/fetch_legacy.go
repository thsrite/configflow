package rulefetch

import (
	"context"
	"time"

	"configflow/internal/jsonx"
)

// Request is the keyword arguments of request_rule.
type Request struct {
	// Method is "get" (default when "") or "head".
	Method string
	// Config is config_data; nil reads get_system_config().
	Config *jsonx.Object
	// MaxRedirects is max_redirects; nil means 3.
	MaxRedirects *int
	// MaxBytes > 0 reads the body (bounded) inside the retry loop; 0 is None.
	MaxBytes int64
	// Stream keeps the body lazy (stream=True).
	Stream                      bool
	ConnectTimeout, ReadTimeout time.Duration
	Insecure                    bool   // verify=False
	CABundle                    string // verify=<path>
	// HostURL is flask.request.host_url when called inside a request ("" outside).
	HostURL string
}

// RequestRule is the temporary adapter for unported download callers.
func (c *Client) RequestRule(ctx context.Context, url string, r Request) (*Response, error) {
	return c.Download(ctx, url, DownloadRequest{Method: r.Method, Config: sourceConfig(r.Config), MaxRedirects: r.MaxRedirects, MaxBytes: r.MaxBytes, Stream: r.Stream, ConnectTimeout: r.ConnectTimeout, ReadTimeout: r.ReadTimeout, Insecure: r.Insecure, CABundle: r.CABundle, HostURL: r.HostURL})
}

// RequestRule is DefaultClient.RequestRule.
func RequestRule(ctx context.Context, url string, r Request) (*Response, error) {
	return DefaultClient.RequestRule(ctx, url, r)
}

// IsInternalRuleURL adapts URL classification for validated URLs.
func IsInternalRuleURL(url string, config *jsonx.Object, host string) bool {
	ok, _ := InternalURL(url, sourceConfig(config), host)
	return ok
}

// GithubDownloadCandidates adapts the existing converter boundary.
func GithubDownloadCandidates(url string, config *jsonx.Object, host string) ([]Candidate, error) {
	return DownloadCandidates(url, sourceConfig(config), host)
}

// NormalizeRuleFetchProxy rejects non-string request values before native validation.
func NormalizeRuleFetchProxy(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", ErrProxyNotString
	}
	return NormalizeProxy(text)
}
