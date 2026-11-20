package jsonstore

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/testjson"
)

// Ported from backend/test_domain_status_cache.py (the json_store tests); the
// Python monkeypatching of os.replace becomes the afterReplace hook.

func sample() *testjson.Object {
	return testjson.Obj(
		testjson.Field{Key: "status", Value: testjson.Obj(testjson.Field{Key: "value", Value: "running"}, testjson.Field{Key: "name", Value: "例子 é"})},
		testjson.Field{Key: "days", Value: map[string]any{}},
		testjson.Field{Key: "n", Value: []any{1, 2.5}},
	)
}

func TestWriteBytesEqualPythonsAndReadBack(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "nested"))
	sig, err := store.Write("a", sample(), false)
	if err != nil || sig != nil {
		t.Fatalf("write: %v %v", sig, err)
	}
	data, err := os.ReadFile(store.Path("a"))
	if err != nil {
		t.Fatal(err)
	}
	// json.dump(..., ensure_ascii=False, separators=(',', ':')), no newline.
	want := `{"status":{"value":"running","name":"例子 é"},"days":{},"n":[1,2.5]}`
	if string(data) != want {
		t.Errorf("got %q", data)
	}
	if !testjson.Equal(readObject(store, "a"), sample()) {
		t.Error("read back differs")
	}
	if _, err := os.Stat(store.Path("a") + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}

func TestOtherStoresKeepCloseBeforeReplaceWithoutSignature(t *testing.T) {
	store := New(t.TempDir())
	sig, err := store.Write("a", map[string]any{"hosts": map[string]any{"example.test": map[string]any{"value": "unchanged"}}}, false)
	if sig != nil || err != nil {
		t.Fatalf("%v %v", sig, err)
	}
	if got, _ := testjson.Path(readObject(store, "a"), "hosts", "example.test", "value"); got != "unchanged" {
		t.Errorf("got %q", got)
	}
}

func TestPathSanitisesSeparators(t *testing.T) {
	store := New("/data")
	if got := store.Path(`a/b\c`); got != "/data/a_b_c.json" {
		t.Errorf("got %q", got)
	}
}

func TestMissingAndDamagedFilesReadAsNil(t *testing.T) {
	store := New(t.TempDir())
	if readObject(store, "missing") != nil {
		t.Error("missing file")
	}
	for name, content := range map[string]string{"bad": "{", "list": "[]", "str": `"x"`, "empty": "", "bom": "\ufeff{}", "latin": "{\"a\":\"\xe9\"}"} {
		if err := os.WriteFile(store.Path(name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if readObject(store, name) != nil {
			t.Errorf("%s should read as nil", name)
		}
	}
	if err := os.WriteFile(store.Path("ok"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readObject(store, "ok"); got == nil || len(got) != 0 {
		t.Errorf("ok: %v", got)
	}
}

func TestRemoveIgnoresMissingFiles(t *testing.T) {
	store := New(t.TempDir())
	if err := store.Remove("never"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write("a", sample(), false); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove("a"); err != nil {
		t.Fatal(err)
	}
	if readObject(store, "a") != nil {
		t.Error("file still readable")
	}
}

func TestVerifiedWriteReturnsTheSignatureOfTheWrittenFile(t *testing.T) {
	store := New(t.TempDir())
	sig, err := store.Write("a", sample(), true)
	if err != nil || sig == nil {
		t.Fatalf("%v %v", sig, err)
	}
	info, _ := os.Stat(store.Path("a"))
	if *sig != signatureOf(info) {
		t.Errorf("signature %v differs from the file's %v", *sig, signatureOf(info))
	}
}

func TestExternalReplacementDuringVerifiedWriteDoesNotChangeWrittenIdentity(t *testing.T) {
	store := New(t.TempDir())
	store.afterReplace = func(path string) {
		external := strings.TrimSuffix(path, ".json") + ".external"
		_ = os.WriteFile(external, []byte(`{"status":{"value":"unreachable"},"days":{}}`), 0o644)
		_ = os.Rename(external, path)
	}
	sig, err := store.Write("a", sample(), true)
	if err != nil {
		t.Fatal(err)
	}
	// The descriptor still refers to the replaced inode: the signature is
	// that of the written file, which is no longer the file at the path.
	info, _ := os.Stat(store.Path("a"))
	if sig == nil || *sig == signatureOf(info) {
		t.Errorf("signature must describe the old inode: %v vs %v", sig, signatureOf(info))
	}
	if got, _ := testjson.Path(readObject(store, "a"), "status", "value"); got != "unreachable" {
		t.Errorf("got %q", got)
	}
}

func TestExternalInPlaceEditDuringVerifiedWriteIsNotSigned(t *testing.T) {
	for _, preserveMtime := range []bool{false, true} {
		store := New(t.TempDir())
		store.afterReplace = func(path string) {
			previous, _ := os.Stat(path)
			// Same size, different bytes.
			data, _ := os.ReadFile(path)
			edited := strings.Replace(string(data), "running", "unknown", 1)
			if len(edited) != len(data) {
				edited = strings.Replace(string(data), "running", "runnin_", 1)
			}
			_ = os.WriteFile(path, []byte(edited), 0o644)
			stamp := previous.ModTime()
			if !preserveMtime {
				stamp = stamp.Add(time.Second)
			}
			_ = os.Chtimes(path, stamp, stamp)
		}
		sig, err := store.Write("a", sample(), true)
		if err != nil {
			t.Fatal(err)
		}
		if sig != nil {
			t.Errorf("preserveMtime=%v: an edited file got a signature", preserveMtime)
		}
		if got, _ := testjson.Path(readObject(store, "a"), "status", "value"); got == "running" {
			t.Errorf("edit not applied: %q", got)
		}
	}
}

func TestWriteFailureLeavesTheOldFile(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Write("a", sample(), false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Write("a", map[string]any{"x": math.NaN()}, false); err == nil {
		t.Fatal("a non-JSON value was written")
	}
	if !testjson.Equal(readObject(store, "a"), sample()) {
		t.Error("the old file changed")
	}
}

func readObject(s *Store, id string) map[string]any {
	var value map[string]any
	if !s.Read(id, &value) {
		return nil
	}
	return value
}
