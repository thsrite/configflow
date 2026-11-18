package rulefetch

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"configflow/internal/repo"
	"configflow/internal/store"
)

// pythonClassOf is type(err).__name__ for the errors C14's code paths can
// see, "" when err carries no Python class (a plain Go error of this
// package, which stands for ValueError).
func pythonClassOf(err error) string {
	var classed interface{ PythonClass() string }
	var download *DownloadError
	var remote *RemoteRuleValidationError
	var repoErr *repo.Error
	var storeErr *store.Error
	var pathErr *fs.PathError
	switch {
	case errors.As(err, &download):
		return "RequestException"
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
