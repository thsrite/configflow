package refresh

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"math/big"
	"os"
	"regexp"
	"strings"
	"syscall"

	"configflow/internal/convert"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rulefetch"
	"configflow/internal/rules"
	"configflow/internal/store"
)

// PyError is an exception Python raises on a value the code does not check
// (TypeError, AttributeError, KeyError, OverflowError, ValueError ...).
type PyError struct {
	Class   string
	Message string
}

func (e *PyError) Error() string       { return e.Message }
func (e *PyError) PythonClass() string { return e.Class }

func pyErr(class, format string, args ...any) *PyError {
	return &PyError{Class: class, Message: fmt.Sprintf(format, args...)}
}

// pyTypeName is type(v).__name__ for a JSON value.
func pyTypeName(v any) string { return httpx.PyTypeName(v) }

// osErrorClass is the OSError subclass Python raises for a file error.
func osErrorClass(err error) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case syscall.ENOENT:
			return "FileNotFoundError"
		case syscall.EACCES, syscall.EPERM:
			return "PermissionError"
		case syscall.EISDIR:
			return "IsADirectoryError"
		case syscall.ENOTDIR:
			return "NotADirectoryError"
		case syscall.EEXIST:
			return "FileExistsError"
		}
	}
	return "OSError"
}

// isOSError reports whether err is a Python OSError (a failed file
// operation, or ConfigStorageError) rather than a repository error.
func isOSError(err error) bool {
	if errors.Is(err, store.ErrStorage) {
		return true
	}
	var pathErr *fs.PathError
	var linkErr *os.LinkError
	var errno syscall.Errno
	return errors.As(err, &pathErr) || errors.As(err, &linkErr) || errors.As(err, &errno)
}

// pyOSError is str(OSError) for a failed file operation on path.
func pyOSError(err error, path string) string {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		text := errno.Error()
		if text != "" {
			text = strings.ToUpper(text[:1]) + text[1:]
		}
		return fmt.Sprintf("[Errno %d] %s: %s", int(errno), text, jsonx.Repr(path))
	}
	return err.Error()
}

// errorText is str(exc) for errors of the repository and the file system.
func errorText(err error) string {
	var storeErr *store.Error
	if errors.As(err, &storeErr) {
		return storeErr.Error()
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pyOSError(pathErr.Err, pathErr.Path)
	}
	var linkErr *os.LinkError
	if errors.As(err, &linkErr) {
		return pyOSError(linkErr.Err, linkErr.Old) + " -> " + jsonx.Repr(linkErr.New)
	}
	return err.Error()
}

// isRepositoryOrOSError is `except (OSError, ProfileRepositoryError)`.
func isRepositoryOrOSError(err error) bool {
	return errors.Is(err, repo.ErrRepository) || isOSError(err)
}

var repoClasses = []struct {
	kind  error
	class string
}{
	{repo.ErrNodeDeletionChanged, "NodeDeletionChanged"},
	{repo.ErrValidation, "ProfileValidationError"},
	{repo.ErrNotFound, "ProfileNotFound"},
	{repo.ErrExists, "ProfileExists"},
	{repo.ErrInUse, "ProfileInUse"},
	{repo.ErrRuleLibraryCreationUncertain, "RuleLibraryCreationUncertain"},
}

var storeClasses = []struct {
	kind  error
	class string
}{
	{store.ErrStorage, "ConfigStorageError"},
	{store.ErrCorrupt, "StoreCorrupt"},
	{store.ErrUnsupported, "StoreUnsupported"},
	{store.ErrConflict, "StoreConflict"},
}

// pythonClass is type(err).__name__. Only err itself decides (Python reads
// the class of the exception object, never of its __cause__). Go errors
// without a Python counterpart are named by their Go type, like
// urlx.SafeExceptionDetails.
func pythonClass(err error) string {
	switch e := err.(type) {
	case *convert.PyError:
		return e.Class
	case *convert.DialerReferenceError:
		return "DialerReferenceError"
	case *convert.EasyTierSettingsError:
		return "EasyTierSettingsError"
	case *rules.StrategyReferenceError:
		return "StrategyReferenceError"
	case *rulefetch.RemoteRuleValidationError:
		return "RemoteRuleValidationError"
	case *repo.Error:
		for _, c := range repoClasses {
			if e.Kind == c.kind {
				return c.class
			}
		}
		return "ProfileRepositoryError"
	case *store.Error:
		for _, c := range storeClasses {
			if errors.Is(e, c.kind) {
				return c.class
			}
		}
		return "StoreError"
	case interface{ PythonClass() string }:
		return e.PythonClass()
	}
	if isOSError(err) {
		return osErrorClass(err)
	}
	return fmt.Sprintf("%T", err)
}

// requestsBases is the requests exception hierarchy (class → direct bases)
// for isinstance checks on class names.
var requestsBases = map[string][]string{
	"HTTPError":             {"RequestException"},
	"ConnectionError":       {"RequestException"},
	"ProxyError":            {"ConnectionError"},
	"SSLError":              {"ConnectionError"},
	"Timeout":               {"RequestException"},
	"ConnectTimeout":        {"ConnectionError", "Timeout"},
	"ReadTimeout":           {"Timeout"},
	"URLRequired":           {"RequestException"},
	"TooManyRedirects":      {"RequestException"},
	"MissingSchema":         {"RequestException", "ValueError"},
	"InvalidSchema":         {"RequestException", "ValueError"},
	"InvalidURL":            {"RequestException", "ValueError"},
	"InvalidHeader":         {"RequestException", "ValueError"},
	"InvalidProxyURL":       {"InvalidURL"},
	"InvalidJSONError":      {"RequestException"},
	"JSONDecodeError":       {"InvalidJSONError", "ValueError"},
	"ChunkedEncodingError":  {"RequestException"},
	"ContentDecodingError":  {"RequestException"},
	"StreamConsumedError":   {"RequestException", "TypeError"},
	"RetryError":            {"RequestException"},
	"UnrewindableBodyError": {"RequestException"},
	"RuleFetchError":        {"RequestException"},
}

// isSubclass reports whether class is base or derives from it in the
// requests hierarchy.
func isSubclass(class, base string) bool {
	if class == base {
		return true
	}
	for _, parent := range requestsBases[class] {
		if isSubclass(parent, base) {
			return true
		}
	}
	return false
}

func isInstance(err error, base string) bool { return isSubclass(pythonClass(err), base) }

// responseStatus is getattr(getattr(error, 'response', None), 'status_code',
// None) as an int (ok=false for None).
func responseStatus(err error) (int, bool) {
	if s, ok := err.(interface{ HTTPStatus() int }); ok && s.HTTPStatus() != 0 {
		return s.HTTPStatus(), true
	}
	return 0, false
}

var exceptionNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// safeExceptionDetails is url_utils.safe_exception_details(error).
func safeExceptionDetails(err error) string {
	name := exceptionNameChars.ReplaceAllString(pythonClass(err), "")
	if len(name) > 80 {
		name = name[:80]
	}
	if name == "" {
		name = "Exception"
	}
	details := "exception_type=" + name
	if status, ok := responseStatus(err); ok {
		details += fmt.Sprintf(" status=%d", status)
	}
	return details
}

// pyIntValue is int(value) for a JSON value (bool, int, float, str); big
// results are returned whole.
func pyIntValue(value any) (*big.Int, error) {
	switch v := value.(type) {
	case bool:
		if v {
			return big.NewInt(1), nil
		}
		return big.NewInt(0), nil
	case jsonx.Number:
		if n, ok := v.BigInt(); ok {
			return n, nil
		}
		f := v.Float64()
		if math.IsInf(f, 0) {
			return nil, pyErr("OverflowError", "cannot convert float infinity to integer")
		}
		if math.IsNaN(f) {
			return nil, pyErr("ValueError", "cannot convert float NaN to integer")
		}
		n, _ := new(big.Float).SetFloat64(math.Trunc(f)).Int(nil)
		return n, nil
	case string:
		if n, ok := httpx.PyParseInt(v); ok {
			return n, nil
		}
		return nil, pyErr("ValueError", "invalid literal for int() with base 10: %s", jsonx.Repr(v))
	case nil:
		return nil, pyErr("TypeError", "int() argument must be a string, a bytes-like object or a real number, not 'NoneType'")
	}
	return nil, pyErr("TypeError", "int() argument must be a string, a bytes-like object or a real number, not '%s'", pyTypeName(value))
}

// orDefault is `value or default`.
func orDefault(value, fallback any) any {
	if jsonx.Truthy(value) {
		return value
	}
	return fallback
}
