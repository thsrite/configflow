package agents

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/store"
)

// verifyNativeRemoteReference changes only explicitly bound malformed reply or
// stored-config rows from the actual unchanged Python remote_ref.py oracle.
func verifyNativeRemoteReference(t *testing.T, e *env, name, body string, status int, want any, err error, before store.Head) bool {
	t.Helper()
	metadata, readErr := os.ReadFile("testdata/n810_remote_native.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	var reference struct {
		Archive string `json:"corpus_sha256"`
		Rows    []struct {
			Name   string `json:"name"`
			Body   string `json:"body"`
			Status int    `json:"status"`
			Class  string `json:"python_class"`
			SHA    string `json:"sha256"`
		} `json:"rows"`
	}
	if json.Unmarshal(metadata, &reference) != nil || reference.Archive != "c04d443c7537db5eea627c1e9b7c24256b4a3adf70e43be769ad8e83db16c357" || len(reference.Rows) != 9 {
		t.Fatal("invalid independent remote metadata")
	}
	for _, row := range reference.Rows {
		if row.Name != name {
			continue
		}
		archive, readErr := os.ReadFile("testdata/n810_remote_python.json")
		if readErr != nil || fmt.Sprintf("%x", sha256.Sum256(archive)) != reference.Archive {
			t.Fatal("original remote Python archive changed", readErr)
		}
		value, parseErr := jsonx.Parse(archive)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		original := value.(*jsonx.Object).GetObject(name)
		raw, encodeErr := jsonx.Compact(original)
		if encodeErr != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != row.SHA || row.Body != body || row.Status != status {
			t.Fatalf("original remote input/output row changed: %s", name)
		}
		if text, present := original.Value("error").(string); present {
			class, message, _ := strings.Cut(text, ": ")
			if class != row.Class || message != want {
				t.Fatalf("original exception class/text changed: %s", name)
			}
		} else if !jsonx.Equal(original.Value("result"), want) {
			t.Fatalf("original Python result changed: %s", name)
		}
		if strings.HasPrefix(name, "logging_existing_") {
			if row.Class != "TypeError" || !repo.IsValueError(err) {
				t.Fatalf("stored logging shape was not refused: %v", err)
			}
		} else if !errors.Is(err, ErrInvalidAgentReply) {
			t.Fatalf("upstream shape was not refused: %v", err)
		}
		after, headErr := e.repo.Head(e.ctx)
		if headErr != nil || before != after {
			t.Fatalf("malformed remote reply wrote transaction: before=%v after=%v err=%v", before, after, headErr)
		}
		return true
	}
	return false
}

func remoteReferenceHead(t *testing.T, e *env) store.Head {
	t.Helper()
	head, err := e.repo.Head(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return head
}

func verifyNativeCallPreparation(t *testing.T, e *env, name, want string, err error, before store.Head) {
	t.Helper()
	data, readErr := os.ReadFile("testdata/n810_call_preparation.json")
	if readErr != nil {
		t.Fatal(readErr)
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != "d18aedee323d96e6fccd37e39e118a7a7f8ee60a8167751495c2d1e7df061996" {
		t.Fatal("original preparation rows changed")
	}
	value, parseErr := jsonx.Parse(data)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	for _, item := range value.([]any) {
		entry := item.(*jsonx.Object)
		row := entry.GetObject("row")
		if row.GetString("name") != name {
			continue
		}
		raw, encodeErr := jsonx.Compact(row)
		if encodeErr != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != entry.GetString("sha256") || row.GetString("python_message") != want {
			t.Fatal("original preparation row input/output changed", name)
		}
		missing := map[string]bool{}
		for _, field := range row.GetList("missing") {
			missing[field.(string)] = true
		}
		stored := e.stored(e.id())
		for _, field := range []string{"host", "port", "token"} {
			if stored.Has(field) == missing[field] {
				t.Fatalf("original preparation input changed: %s %s", name, field)
			}
		}
		switch row.GetString("python_class") {
		case "UnicodeEncodeError":
			if stored.GetString("token") != row.GetString("token") || !errors.Is(err, ErrHeaderEncoding) {
				t.Fatal("header preparation did not reject", name, err)
			}
		case "KeyError":
			if !errors.Is(err, ErrAgentEndpoint) {
				t.Fatal("endpoint preparation did not reject", name, err)
			}
		default:
			t.Fatal("unreviewed original preparation error class", name)
		}
		after, headErr := e.repo.Head(e.ctx)
		if headErr != nil || before != after || IsTransportError(err) {
			t.Fatalf("preparation changed transaction or transport class: %s %v", name, err)
		}
		return
	}
	t.Fatal("unbound original preparation row", name)
}
