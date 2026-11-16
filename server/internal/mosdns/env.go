package mosdns

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
)

// Env holds the network I/O of the archive and the rule proxy. Tests
// install fakes with SetEnv.
type Env struct {
	// RequestRule is rule_fetch.request_rule (nil: rulefetch.RequestRule).
	RequestRule func(ctx context.Context, url string, r rulefetch.Request) (*rulefetch.Response, error)
	// FetchRemoteContent replaces routes.mosdns._fetch_remote_content
	// (nil: FetchRemoteContent on RequestRule).
	FetchRemoteContent func(ctx context.Context, url string, config *jsonx.Object, raw bool, hostURL string) (any, error)
}

var (
	envMu sync.Mutex
	env   = &Env{}
)

func currentEnv() *Env {
	envMu.Lock()
	defer envMu.Unlock()
	return env
}

// SetEnv replaces the environment and returns a function restoring the
// previous one.
func SetEnv(e *Env) (restore func()) {
	envMu.Lock()
	previous := env
	env = e
	envMu.Unlock()
	return func() {
		envMu.Lock()
		env = previous
		envMu.Unlock()
	}
}

func (e *Env) requestRule(ctx context.Context, url string, r rulefetch.Request) (*rulefetch.Response, error) {
	if e.RequestRule != nil {
		return e.RequestRule(ctx, url, r)
	}
	return rulefetch.RequestRule(ctx, url, r)
}

// fetchRemoteContent calls the replacement or FetchRemoteContent. config is
// nil for Python's config_data=None.
func (e *Env) fetchRemoteContent(ctx context.Context, url string, config *jsonx.Object, raw bool, hostURL string) (any, error) {
	if e.FetchRemoteContent != nil {
		return e.FetchRemoteContent(ctx, url, config, raw, hostURL)
	}
	return FetchRemoteContent(ctx, url, config, raw, hostURL)
}

// pythonClass is type(err).__name__ for the errors this module sees.
// Plain Go errors count as ValueError (rulefetch's convention).
func pythonClass(err error) string { return rulefetch.PythonClass(err) }

var valueErrorClasses = map[string]bool{
	"ValueError": true, "UnicodeDecodeError": true, "UnicodeEncodeError": true, "UnicodeError": true,
	"ProfileValidationError": true, "ProfileExists": true, "ProfileInUse": true, "NodeDeletionChanged": true,
	"RemoteRuleValidationError": true, "MosdnsArchiveError": true, "JSONDecodeError": true,
	"MissingSchema": true, "InvalidSchema": true, "InvalidURL": true, "InvalidHeader": true,
	"InvalidProxyURL": true, "OxidnsSettingsError": true, "StrategyReferenceError": true,
	"DialerReferenceError": true,
}

// isValueError is isinstance(err, ValueError).
func isValueError(err error) bool {
	if repo.IsValueError(err) {
		return true
	}
	return valueErrorClasses[pythonClass(err)]
}

// isRequestException is isinstance(err, requests.RequestException).
func isRequestException(err error) bool {
	if rulefetch.IsRequestException(err) {
		return true
	}
	switch pythonClass(err) {
	case "RequestException", "HTTPError", "ConnectionError", "ProxyError", "SSLError", "Timeout",
		"ConnectTimeout", "ReadTimeout", "URLRequired", "TooManyRedirects", "MissingSchema", "InvalidSchema",
		"InvalidURL", "InvalidHeader", "InvalidProxyURL", "InvalidJSONError", "ChunkedEncodingError",
		"ContentDecodingError", "StreamConsumedError", "RetryError", "UnrewindableBodyError", "RuleFetchError":
		return true
	}
	return false
}

var exceptionNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// safeExceptionDetails is url_utils.safe_exception_details(err): the class
// and, when the exception itself carries a response, its status.
func safeExceptionDetails(err error) string {
	name := exceptionNameChars.ReplaceAllString(pythonClass(err), "")
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "Exception"
	}
	details := "exception_type=" + name
	if s, ok := err.(interface{ HTTPStatus() int }); ok && s.HTTPStatus() != 0 {
		var ruleFetch *rulefetch.RuleFetchError
		if !errors.As(err, &ruleFetch) {
			details += " status=" + strconv.Itoa(s.HTTPStatus())
		}
	}
	return details
}
