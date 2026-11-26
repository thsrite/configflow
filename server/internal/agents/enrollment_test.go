package agents

import (
	"errors"
	"testing"

	"configflow/internal/repo"
)

func TestTypedEnrollmentRejectsNonTextProfileBindings(t *testing.T) {
	for _, raw := range []string{`false`, `0`, `[]`, `{}`, `true`, `7`} {
		if _, err := DecodeRegistrationInput([]byte(`{"profile_id":` + raw + `}`)); !errors.Is(err, repo.ErrValidation) {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
	for _, raw := range []string{`null`, `""`, `"default"`} {
		if _, err := DecodeRegistrationInput([]byte(`{"profile_id":` + raw + `}`)); err != nil {
			t.Fatalf("raw=%s err=%v", raw, err)
		}
	}
}
