package urlx

// Exported access to the urllib.parse port above, for packages that parse
// URLs the way Python does (oxidns_monitor's upstream labels).

// SplitResult is urllib.parse.SplitResult for a str URL.
type SplitResult struct {
	Scheme, Netloc, Path, Query, Fragment string
}

// Split is urllib.parse.urlsplit(url) (Python 3.11.17); the error is the
// ValueError urlsplit raises.
func Split(url string) (SplitResult, error) {
	r, err := urlsplit(url)
	if err != nil {
		return SplitResult{}, err
	}
	return SplitResult{r.scheme, r.netloc, r.path, r.query, r.fragment}, nil
}

func (r SplitResult) inner() splitResult {
	return splitResult{r.Scheme, r.Netloc, r.Path, r.Query, r.Fragment}
}

// Hostname is SplitResult.hostname: lower-cased (Python's str.lower), a
// zone kept as it is; "" for None.
func (r SplitResult) Hostname() string { return r.inner().hostname() }

// Port is SplitResult.port: ok false for None, err the ValueError.
func (r SplitResult) Port() (port int, ok bool, err error) { return r.inner().port() }

// Unsplit is urllib.parse.urlunsplit((scheme, netloc, path, query, fragment)).
func Unsplit(scheme, netloc, path, query, fragment string) string {
	return urlunsplit(scheme, netloc, path, query, fragment)
}
