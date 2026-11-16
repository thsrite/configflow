package rulefetch

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"configflow/internal/pyyaml"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/urlx"
)

// pythonClassOf is type(err).__name__ for the errors C14's code paths can
// see, "" when err carries no Python class (a plain Go error of this
// package, which stands for ValueError).
func pythonClassOf(err error) string {
	var classed interface{ PythonClass() string }
	var remote *RemoteRuleValidationError
	var recursion *pyyaml.RecursionError
	var repoErr *repo.Error
	var storeErr *store.Error
	var pathErr *fs.PathError
	switch {
	case errors.As(err, &remote):
		return "RemoteRuleValidationError"
	case errors.As(err, &classed):
		return classed.PythonClass()
	case errors.As(err, &repoErr):
		switch {
		case errors.Is(err, repo.ErrNodeDeletionChanged):
			return "NodeDeletionChanged"
		case errors.Is(err, repo.ErrValidation):
			return "ProfileValidationError"
		case errors.Is(err, repo.ErrNotFound):
			return "ProfileNotFound"
		case errors.Is(err, repo.ErrExists):
			return "ProfileExists"
		case errors.Is(err, repo.ErrInUse):
			return "ProfileInUse"
		case errors.Is(err, repo.ErrRuleLibraryCreationUncertain):
			return "RuleLibraryCreationUncertain"
		}
		return "ProfileRepositoryError"
	case errors.As(err, &storeErr):
		switch {
		case errors.Is(err, store.ErrStorage):
			return "ConfigStorageError"
		case errors.Is(err, store.ErrCorrupt):
			return "StoreCorrupt"
		case errors.Is(err, store.ErrUnsupported):
			return "StoreUnsupported"
		case errors.Is(err, store.ErrConflict):
			return "StoreConflict"
		case errors.Is(err, store.ErrSQLite):
			return "OperationalError"
		}
		return "StoreError"
	case errors.As(err, &recursion):
		return "RecursionError"
	case errors.As(err, &pathErr):
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return "FileNotFoundError"
		case errors.Is(err, fs.ErrPermission):
			return "PermissionError"
		case errors.Is(err, syscall.EISDIR):
			return "IsADirectoryError"
		}
		return "OSError"
	}
	return ""
}

// isValueError is isinstance(err, ValueError) for those errors (plain errors
// of this package are ValueErrors: URL syntax, redirects, the size limit).
func isValueError(err error) bool {
	switch pythonClassOf(err) {
	case "", "ValueError", "UnicodeDecodeError", "UnicodeEncodeError", "RemoteRuleValidationError",
		"ProfileValidationError", "ProfileExists", "ProfileInUse", "NodeDeletionChanged":
		return true
	}
	return false
}

// safeExceptionDetails is url_utils.safe_exception_details: the class and,
// for an exception carrying a response (an HTTPError of raise_for_status),
// its status. A RuleFetchError has no response even when its last attempt
// had one.
func safeExceptionDetails(err error) string {
	details := "exception_type=" + PythonClass(err)
	var fetch *FetchError
	if errors.As(err, &fetch) && fetch.Status != 0 {
		var ruleFetch *RuleFetchError
		if !errors.As(err, &ruleFetch) {
			details += " status=" + itoa(fetch.Status)
		}
	}
	return details
}

func itoa(n int) string { return strconv.Itoa(n) }

// latin1EncodeError is str(UnicodeEncodeError) of text.encode('latin1'),
// "" when text encodes.
func latin1EncodeError(text string) string {
	runes := []rune(text)
	for start, r := range runes {
		if r <= 0xff {
			continue
		}
		end := start + 1
		for end < len(runes) && runes[end] > 0xff {
			end++
		}
		if end-start == 1 {
			escaped := fmt.Sprintf(`\u%04x`, r)
			if r > 0xffff {
				escaped = fmt.Sprintf(`\U%08x`, r)
			}
			return fmt.Sprintf("'latin-1' codec can't encode character '%s' in position %d: ordinal not in range(256)", escaped, start)
		}
		return fmt.Sprintf("'latin-1' codec can't encode characters in position %d-%d: ordinal not in range(256)", start, end-1)
	}
	return ""
}

// basicAuthHeader is requests.auth._basic_auth_str (latin-1; username first).
func basicAuthHeader(user, password string) (string, error) {
	for _, part := range []string{user, password} {
		if message := latin1EncodeError(part); message != "" {
			return "", &pyOSError{class: "UnicodeEncodeError", message: message}
		}
	}
	return "Basic " + base64.StdEncoding.EncodeToString(latin1Bytes(user+":"+password)), nil
}

// urlBasicAuth is get_auth_from_url(prepared URL) as prepare_auth uses it:
// unquoted user and password, ok false when neither is given or the URL has
// no password (unquote(None) is a TypeError there, giving two empty strings).
func urlBasicAuth(prepared string) (string, string, bool) {
	parsed, err := urlparse(prepared, "")
	if err != nil {
		return "", "", false
	}
	user, hasUser, password, hasPassword := parsed.userinfo()
	if !hasUser || !hasPassword {
		return "", "", false
	}
	user, password = urlx.Unquote(user), urlx.Unquote(password)
	if user == "" && password == "" {
		return "", "", false
	}
	return user, password, true
}

// proxyCredentialsError is the UnicodeEncodeError proxy_headers raises for
// credentials of the proxy URL that latin-1 cannot encode (decoded as
// urllib3 re-encodes them: literally when any "%" is not an escape).
func proxyCredentialsError(proxy string) error {
	authority := proxy
	if _, rest, ok := strings.Cut(authority, "://"); ok {
		authority = rest
	}
	if i := strings.IndexAny(authority, "/?#"); i >= 0 {
		authority = authority[:i]
	}
	auth := ""
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		auth = authority[:i]
	}
	user, password, _ := strings.Cut(auth, ":")
	if strings.Count(auth, "%") == len(percentEscape.FindAllString(auth, -1)) {
		user, password = urlx.Unquote(user), urlx.Unquote(password)
	}
	for _, part := range []string{user, password} {
		if message := latin1EncodeError(part); message != "" {
			return &pyOSError{class: "UnicodeEncodeError", message: message}
		}
	}
	return &pyOSError{class: "UnicodeEncodeError", message: "'latin-1' codec can't encode the proxy credentials"}
}

var percentEscape = regexp.MustCompile(`%[0-9A-Fa-f]{2}`)
