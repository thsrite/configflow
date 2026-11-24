package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"configflow/internal/store"
)

func xcErrorClass(err error) string {
	var failure *Error
	if errors.As(err, &failure) {
		switch failure.Kind {
		case ErrValidation:
			return "ProfileValidationError"
		case ErrNotFound:
			return "ProfileNotFound"
		case ErrExists:
			return "ProfileExists"
		case ErrInUse:
			return "ProfileInUse"
		case ErrNodeDeletionChanged:
			return "NodeDeletionChanged"
		case ErrRuleLibraryCreationUncertain:
			return "RuleLibraryCreationUncertain"
		}
		return "ProfileRepositoryError"
	}
	var missing *MissingError
	if errors.As(err, &missing) {
		return "KeyError"
	}
	if errors.Is(err, store.ErrStorage) {
		return "ConfigStorageError"
	}
	return fmt.Sprintf("%T", err)
}

func xcFingerprint(t *testing.T, s *store.Store) []any {
	t.Helper()
	connection := must(sql.Open("sqlite", s.Path()))(t)
	defer connection.Close()
	var tables []any
	for _, query := range []string{"SELECT * FROM meta ORDER BY key", "SELECT * FROM sections ORDER BY scope, owner", "SELECT * FROM fields ORDER BY scope, owner, field"} {
		rows := must(connection.Query(query))(t)
		columns := must(rows.Columns())(t)
		var values []any
		for rows.Next() {
			row := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range row {
				pointers[i] = &row[i]
			}
			noError(t, rows.Scan(pointers...))
			values = append(values, row)
		}
		noError(t, rows.Err())
		rows.Close()
		tables = append(tables, values)
	}
	return tables
}
