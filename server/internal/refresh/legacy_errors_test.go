package refresh

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
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

func (e *PyError) Error() string      { return e.Message }
func (e *PyError) ErrorClass() string { return e.Class }

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
// weburl.SafeErrorDetails.
func pythonClass(err error) string {
	if errors.Is(err, ErrScheduleTime) {
		return "OverflowError"
	}
	if errors.Is(err, ErrFailureCount) {
		return "ValueError"
	}
	switch e := err.(type) {
	case *convert.PyError:
		return e.Class
	case *convert.DialerReferenceError:
		return "DialerReferenceError"
	case *convert.EasyTierSettingsError:
		return "EasyTierSettingsError"
	case *rules.StrategyReferenceError:
		return "StrategyReferenceError"
	case *rulefetch.FetchError:
		return e.Class
	case *rulefetch.RuleFetchError:
		return "RuleFetchError"
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
	case interface{ ErrorClass() string }:
		return e.ErrorClass()
	}
	if isOSError(err) {
		return osErrorClass(err)
	}
	return fmt.Sprintf("%T", err)
}
