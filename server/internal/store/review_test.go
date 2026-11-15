package store

// Regression tests added by the C01 adversarial review. Expected values are
// CPython 3.11 results of backend/common/config_store.py (generated on the
// test server; the same scenarios run live in TestCrossCheckPythonErrors).

import (
	"errors"
	"testing"

	"configflow/internal/jsonx"
)

// TestInvalidUTF8TextIsAStorageError: Python's sqlite3 cannot decode a
// TEXT value that is not UTF-8 and raises OperationalError, which reads
// translate to ConfigStorageError and check() lets through.
func TestInvalidUTF8TextIsAStorageError(t *testing.T) {
	cases := []struct {
		name, statement string
		load, check     string // "" = succeeds
	}{
		{"value", "UPDATE fields SET value = CAST(X'5B22FF2041E4B8225D' AS TEXT) WHERE scope = 'system' AND field = 'agents'",
			"配置数据库访问失败：Could not decode to UTF-8 column 'value' with text '[\"\ufffd A\ufffd\ufffd\"]'", ""},
		{"meta", "UPDATE meta SET value = CAST(X'22F09F9822' AS TEXT) WHERE key = 'migration'",
			"", "Could not decode to UTF-8 column 'value' with text '\"\ufffd\ufffd\ufffd\"'"},
		{"meta-head", "UPDATE meta SET value = CAST(X'22F09F9822' AS TEXT) WHERE key = 'store_id'",
			"配置数据库访问失败：Could not decode to UTF-8 column 'value' with text '\"\ufffd\ufffd\ufffd\"'",
			"Could not decode to UTF-8 column 'value' with text '\"\ufffd\ufffd\ufffd\"'"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			target := newStore(t)
			must(target.Create(sampleDocument(t), jsonx.Obj("migration", jsonx.Obj("source", "defaults"))))(t)
			execSQL(t, target.Path(), item.statement)
			_, err := target.Load()
			assertKind(t, "load", err, ErrStorage, item.load)
			_, err = target.Fingerprint()
			if item.load != "" || item.name == "meta" {
				want := item.load
				if want == "" {
					want = "配置数据库访问失败：" + item.check
				}
				assertKind(t, "fingerprint", err, ErrStorage, want)
			}
			assertKind(t, "check", target.Check(), ErrSQLite, item.check)
			if item.check != "" && target.SalvageMeta() != nil {
				t.Error("SalvageMeta: want nil like Python's None")
			}
		})
	}
}

func assertKind(t *testing.T, what string, err, kind error, message string) {
	t.Helper()
	if message == "" {
		if err != nil {
			t.Errorf("%s: %v, Python succeeds", what, err)
		}
		return
	}
	var typed *Error
	if !errors.As(err, &typed) || typed.Kind != kind || err.Error() != message {
		t.Errorf("%s:\n got %v (%v)\nwant %s (%v)", what, err, kindOf(err), message, kind)
	}
}

// TestTranslateKeepsClassifiedErrors: Python's `except sqlite3.Error` in
// _translate never sees a StoreError, even one caused by a SQLite failure,
// so an *Error passes through translate and untranslated unchanged.
func TestTranslateKeepsClassifiedErrors(t *testing.T) {
	target := newStore(t)
	must(target.Create(sampleDocument(t), nil))(t)
	connection := must(connect(target.Path(), false, 0))(t)
	defer connection.close()
	cause := connection.exec("SELECT * FROM no_such_table")
	if _, ok := sqliteError(cause); !ok {
		t.Fatalf("want a SQLite error, got %v", cause)
	}
	classified := &Error{Kind: ErrCorrupt, Message: "已分类", Cause: cause}
	if got := translate(classified); got != classified {
		t.Errorf("translate: %v (%v), want the same *Error", got, kindOf(got))
	}
	if got := untranslated(classified); got != classified {
		t.Errorf("untranslated: %v (%v), want the same *Error", got, kindOf(got))
	}
	if got := translate(cause); kindOf(got) != ErrStorage || got.Error() != "配置数据库访问失败：no such table: no_such_table" {
		t.Errorf("translate(raw): %v (%v)", got, kindOf(got))
	}
}
