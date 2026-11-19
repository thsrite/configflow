package agents

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"modernc.org/sqlite"
)

func verifyNativeRuntimeErrorRow(t *testing.T, index int, row *jsonx.Object, err error) {
	t.Helper()
	data, readErr := os.ReadFile("testdata/n810_runtime_native.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	var reference struct {
		Archive string `json:"corpus_sha256"`
		Rows    []struct {
			Index int    `json:"index"`
			SHA   string `json:"sha256"`
			Class string `json:"python_class"`
		} `json:"rows"`
	}
	if json.Unmarshal(data, &reference) != nil || reference.Archive != "82281a960707379c8ce0ddc54da84992c81e3820968e55998bf3d7772747368a" || len(reference.Rows) != 7 {
		t.Fatal("invalid independent SQLite reference")
	}
	archive, readErr := os.ReadFile("testdata/a02_reference.json")
	if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(archive)) != reference.Archive {
		t.Fatal("original archive changed", readErr)
	}
	for _, r := range reference.Rows {
		if r.Index != index {
			continue
		}
		raw, e := jsonx.Compact(row)
		if e != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != r.SHA {
			t.Fatalf("original SQLite row %d changed", index)
		}
		var driver *sqlite.Error
		switch index {
		case 2, 3, 4:
			if r.Class != "OperationalError" || !errors.Is(err, ErrLegacyTextEncoding) {
				t.Fatalf("Python=%s native=%v", r.Class, err)
			}
		case 5, 6, 8:
			if r.Class != "OperationalError" || !errors.As(err, &driver) || driver.Code() != 1 {
				t.Fatalf("Python=%s native=%v", r.Class, err)
			}
		case 7:
			if r.Class != "DatabaseError" || !errors.As(err, &driver) || driver.Code() != 26 {
				t.Fatalf("Python=%s native=%v", r.Class, err)
			}
		default:
			t.Fatal("unreviewed SQLite error row", index)
		}
		return
	}
	t.Fatal("native rejected an unbound original SQLite row", index, err)
}

func verifyNativeRemovalReference(t *testing.T, original []any, actual []string, dir string) {
	t.Helper()
	raw, e := jsonx.Compact(original)
	if e != nil {
		t.Fatal(e)
	}
	// The two original warning lines contain the original errno and basename.
	if fmt.Sprintf("%x", sha256.Sum256(raw)) != "c373c50cedc68819edd30ec946aa400cf4484f9630a9e491abd53c32e0cea074" {
		t.Fatal("original unlink log rows changed")
	}
	if len(original) != 2 || len(actual) != 2 {
		t.Fatalf("unlink warnings=%q", actual)
	}
	for index, base := range []string{"runtime.sqlite3-wal", "runtime.sqlite3-shm"} {
		old, ok := original[index].(string)
		if !ok || !strings.Contains(old, "[Errno 21]") || !strings.Contains(old, base) {
			t.Fatal("original Python unlink error class changed")
		}
		if !strings.Contains(actual[index], "删除失败") || !strings.Contains(actual[index], dir+"/"+base) {
			t.Fatalf("missing native unlink diagnostic: %q", actual[index])
		}
		info, err := os.Stat(dir + "/" + base)
		if err != nil || !info.IsDir() {
			t.Fatal("unlink removed a directory", err)
		}
	}
}

func TestNativeLegacyReadErrorsDoNotPersist(t *testing.T) {
	for index, row := range referenceCases(t, "legacy_rows") {
		if len(row.GetList("logs")) == 0 {
			continue
		}
		t.Run(row.GetString("name"), func(t *testing.T) {
			e := newEnv(t)
			path := filepath.Join(e.dir, LegacyDatabase)
			if raw, ok := row.Value("raw").(string); ok {
				if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				for _, statement := range row.GetList("sql") {
					if _, err := db.Exec(statement.(string)); err != nil {
						t.Fatal(err)
					}
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			_, readErr := readLegacyRowsContext(e.ctx, path)
			verifyNativeRuntimeErrorRow(t, index, row, readErr)
			before, err := e.repo.Head(e.ctx)
			if err != nil {
				t.Fatal(err)
			}
			// Simulate a restart after installing the retired state file; New already
			// performed the once-only check before this fixture created that file.
			ResetRuntime()
			state := NewRuntimeState()
			if err := ImportLegacyDatabase(e.ctx, e.repo, state, func(context.Context, map[string]*jsonx.Object) (map[string]bool, error) {
				t.Fatal("invalid old rows attempted a config write")
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			after, err := e.repo.Head(e.ctx)
			if err != nil || before != after || len(state.Entries()) != 0 {
				t.Fatalf("invalid rows changed transaction/state: before=%v after=%v err=%v", before, after, err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("old file not removed", err)
			}
		})
	}
}
