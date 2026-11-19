package rulefetch

import (
	"encoding/base64"
	"errors"
	"io/fs"
	"syscall"

	"configflow/internal/repo"
	"configflow/internal/store"
)

// errorClassOf preserves diagnostic labels while using native error chains.
func errorClassOf(err error) string {
	var classed interface{ ErrorClass() string }
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
		return classed.ErrorClass()
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

// basicAuthHeader encodes HTTP Basic credentials using ISO-8859-1.
func basicAuthHeader(user, password string) (string, error) {
	for _, r := range user + password {
		if r > 255 {
			return "", &credentialError{}
		}
	}
	return "Basic " + base64.StdEncoding.EncodeToString(latin1Bytes(user+":"+password)), nil
}

type credentialError struct{}

func (*credentialError) Error() string {
	return "HTTP Basic credentials must use ISO-8859-1 characters"
}
func (*credentialError) ErrorClass() string { return "UnicodeEncodeError" }

// ErrorClass returns the stable rule-validation diagnostic label.
func (e *RemoteRuleValidationError) ErrorClass() string { return "RemoteRuleValidationError" }

type certificateBundleError struct{ err error }

func (e *certificateBundleError) Error() string {
	return "invalid TLS CA certificate bundle: " + e.err.Error()
}
func (e *certificateBundleError) Unwrap() error      { return e.err }
func (e *certificateBundleError) ErrorClass() string { return "OSError" }
