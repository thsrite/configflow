package routers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// o18T1cAccepted reports why a Python reference record and the Go result may
// differ under the O18 T1c plain-record rules, or "" when they really differ.
// Only router records stored with dirty data qualify (accepted differences,
// behaviour-changes.md O18 T1c):
//   - a member holding "", null or {} is the same as a missing member
//     (optional members are left out; Python wrote them on some paths only);
//   - members a router record does not declare are dropped on read;
//   - a nested record of another JSON type reads as missing (lenient read,
//     with a load warning);
//   - members Go always writes (ok, at, config …) appear with their zero
//     value where the Python record had none.
//
// Normal data (no such members) still compares exactly in sameJSON.
func o18T1cAccepted(t *testing.T, got any, want json.RawMessage) string {
	t.Helper()
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	gotValue, wantValue := decodeLoose(t, data), decodeLoose(t, want)
	category := ""
	if issues, err := model.DecodeStored(want, &model.Router{}, ""); err == nil {
		for _, issue := range issues {
			// Damaged router manifests are kept whole by Go, so only members
			// the Go result lacks are dropped.
			if hasPath(gotValue, issue.Path) {
				continue
			}
			if issue.Kind == model.IssueUnknown && dropPath(wantValue, issue.Path) {
				category = joinCategory(category, "未声明成员丢弃")
			}
			if issue.Kind == model.IssueType && dropPath(wantValue, issue.Path) {
				category = joinCategory(category, "错类型成员读作缺失")
			}
		}
	}
	if testjson.Equal(wantValue, gotValue) {
		return category
	}
	wantValue, gotValue = testjson.WithoutEmptyMembers(wantValue), testjson.WithoutEmptyMembers(gotValue)
	if testjson.Equal(wantValue, gotValue) {
		return joinCategory(category, "空值成员省略")
	}
	if dropZeroOnly(wantValue, gotValue) && testjson.Equal(wantValue, gotValue) {
		return joinCategory(category, "空值成员省略 + 始终写成员补零值")
	}
	return ""
}

// hasPath reports whether an object member exists at a dotted issue path.
func hasPath(value any, path string) bool {
	parts := strings.Split(strings.ReplaceAll(path, "[", ".["), ".")
	for _, part := range parts {
		switch current := value.(type) {
		case map[string]any:
			next, ok := current[part]
			if !ok {
				return false
			}
			value = next
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "[%d]", &index); err != nil || index < 0 || index >= len(current) {
				return false
			}
			value = current[index]
		default:
			return false
		}
	}
	return value != nil // a null member (public view) is a missing one
}

func joinCategory(category, next string) string {
	if strings.Contains(category, next) {
		return category
	}
	if category == "" {
		return next
	}
	return category + " + " + next
}

// dropZeroOnly removes from got the object members want lacks whose value is
// false, 0 or [] (members Go always writes); it reports whether any was removed.
func dropZeroOnly(want, got any) bool {
	removed := false
	switch g := got.(type) {
	case map[string]any:
		w, _ := want.(map[string]any)
		for key, item := range g {
			other, ok := w[key]
			if !ok {
				if object, isObject := item.(map[string]any); isObject {
					removed = dropZeroOnly(nil, object) || removed
				}
				if list, isList := item.([]any); item == false || item == json.Number("0") || isList && len(list) == 0 {
					delete(g, key)
					removed = true
				}
				continue
			}
			removed = dropZeroOnly(other, item) || removed
		}
		// An object that is now empty is a missing member.
		for key, item := range g {
			if object, ok := item.(map[string]any); ok && len(object) == 0 {
				if _, ok := w[key]; !ok {
					delete(g, key)
				}
			}
		}
	case []any:
		w, _ := want.([]any)
		for i := range g {
			if i < len(w) {
				removed = dropZeroOnly(w[i], g[i]) || removed
			}
		}
	}
	return removed
}

func decodeLoose(t *testing.T, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

// dropPath deletes an object member at a dotted issue path ("a.b", list
// indexes "[0]"); it reports whether the member existed.
func dropPath(value any, path string) bool {
	parts := strings.Split(strings.ReplaceAll(path, "[", ".["), ".")
	for i, part := range parts {
		last := i == len(parts)-1
		switch current := value.(type) {
		case map[string]any:
			if last {
				_, ok := current[part]
				delete(current, part)
				return ok
			}
			value = current[part]
		case []any:
			var index int
			if _, err := fmt.Sscanf(part, "[%d]", &index); err != nil || index < 0 || index >= len(current) {
				return false
			}
			value = current[index]
		default:
			return false
		}
	}
	return false
}

// seedStoredRow writes one stored row directly (O18 T1c): input is decoded
// strictly, so data of another type (as an older version or Python may have
// stored it) is seeded below the repository's validating writers.
func seedStoredRow(t *testing.T, r *repo.Repository, scope, owner, field, raw string) {
	t.Helper()
	db, err := sql.Open("sqlite", r.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec("UPDATE fields SET value = ?, revision = revision + 1 WHERE scope = ? AND owner = ? AND field = ?", raw, scope, owner, field)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("seed %s/%s/%s: %d rows %v", scope, owner, field, n, err)
	}
	if _, err := db.Exec("UPDATE meta SET value = CAST(value AS INTEGER) + 1 WHERE key = 'generation'"); err != nil {
		t.Fatal(err)
	}
}
