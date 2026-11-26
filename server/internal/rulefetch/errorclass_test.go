package rulefetch

import (
	"errors"
	"io/fs"
	"syscall"

	"configflow/internal/repo"
	"configflow/internal/store"
)

// errorClassOf names err by the Python exception class the recorded
// references use.
func errorClassOf(err error) string {
	var classed interface{ ErrorClass() string }
	var download *DownloadError
	var remote *RemoteRuleValidationError
	var repoErr *repo.Error
	var storeErr *store.Error
	var pathErr *fs.PathError
	var credential *credentialError
	var bundle *certificateBundleError
	switch {
	case errors.As(err, &credential):
		return "UnicodeEncodeError"
	case errors.As(err, &bundle):
		return "OSError"
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
