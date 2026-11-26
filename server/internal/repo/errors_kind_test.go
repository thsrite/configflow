package repo

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorKindsMatchSentinels(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		rejected bool
		text     string
	}{
		{"validation", validationError("坏值"), true, "坏值"},
		{"exists", exists("已存在"), true, "已存在"},
		{"in use", inUse("使用中"), true, "使用中"},
		{"node deletion changed", newError(ErrNodeDeletionChanged, "已变化"), true, "已变化"},
		{"not found", notFound("p'1"), false, `"p'1"`},
		{"repository", repositoryError("存储失败"), false, "存储失败"},
		{"wrapped validation", fmt.Errorf("save: %w", validationError("坏值")), true, "save: 坏值"},
		{"unknown root kind", &MissingError{Kind: ErrUnknownRootKind, Key: "rulesets"}, false, `"rulesets"`},
		{"missing library rule", &MissingError{Kind: ErrMissingLibraryRule, Key: "r1"}, false, `'r1'`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, ErrRejected); got != tt.rejected {
				t.Fatalf("errors.Is(ErrRejected) = %v, want %v", got, tt.rejected)
			}
			if tt.err.Error() != tt.text {
				t.Fatalf("Error() = %q, want %q", tt.err.Error(), tt.text)
			}
		})
	}
	missing := &MissingError{Kind: ErrMissingLibraryRule, Key: "r1"}
	if !errors.Is(missing, ErrMissingLibraryRule) || errors.Is(missing, ErrUnknownRootKind) || errors.Is(missing, ErrRepository) {
		t.Fatal("MissingError matches the wrong sentinels")
	}
	if !errors.Is(newError(ErrNodeDeletionChanged, "x"), ErrInUse) {
		t.Fatal("NodeDeletionChanged is not an ErrInUse")
	}
}
