package substore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/testjson"
)

type runtimeReferenceFixture struct {
	SourceSHA256   string           `json:"source_sha256"`
	OldPythonBytes string           `json:"old_python_bytes"`
	Expectations   []map[string]any `json:"expectations"`
}

func nativeRuntimeReference(t *testing.T) runtimeReferenceFixture {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_runtime_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/review_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture runtimeReferenceFixture
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(source)
	if fixture.SourceSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("runtime reference source changed")
	}
	if len(fixture.Expectations) != len(reviewList(reviewReference(t), "runtime")) {
		t.Fatal("runtime reference coverage changed")
	}
	return fixture
}

func jsonObjectKeys(data []byte) ([]string, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		return nil, false
	}
	var keys []string
	for dec.More() {
		ktok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		k, ok := ktok.(string)
		if !ok {
			return nil, false
		}
		keys = append(keys, k)
		var discard json.RawMessage
		if err := dec.Decode(&discard); err != nil {
			return nil, false
		}
	}
	return keys, true
}

func compareRuntimeStateFile(t *testing.T, label string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	if !testjson.Equal(json.RawMessage(want), json.RawMessage(got)) {
		t.Errorf("%s: state.json %q want %q", label, got, want)
		return
	}
	a, ok1 := jsonObjectKeys(got)
	b, ok2 := jsonObjectKeys(want)
	if ok1 && ok2 && !slices.Equal(a, b) {
		t.Errorf("%s: state member order changed: %v want %v", label, a, b)
	}
}

func TestN89RuntimeReadsOriginalPythonBytes(t *testing.T) {
	fixture := nativeRuntimeReference(t)
	data := []byte(fixture.OldPythonBytes)
	if !bytes.Contains(data, []byte(`\u`)) || !bytes.Contains(data, []byte(": ")) {
		t.Fatal("fixture is not original Python framing")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(&Env{}, root, 300)
	state, err := runtime.loadState()
	if err != nil || state.State == nil || *state.State != "failed" || state.errorText() != "旧状态" || state.LastUsed == nil || *state.LastUsed != 123.5 {
		t.Fatalf("old state not read: %+v %v", state, err)
	}
	after, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("reading old state rewrote it")
	}
}

func TestN89RuntimeStateCommitLifetimeAndRawMembers(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			before := []byte(nativeRuntimeReference(t).OldPythonBytes)
			root := t.TempDir()
			path := filepath.Join(root, "state.json")
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			owner := NewRuntime(&Env{}, root, 300)
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(context.Background())
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, disconnect := context.WithCancel(runtime.BindRequest(context.Background()))
			defer disconnect()
			entered, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			done := make(chan error, 1)
			go func() {
				done <- owner.withControl(lifecycle.Detach(request), func(ctx context.Context) error {
					return owner.writeStateUsing(ctx, runtimePatch{State: new("running"), LastUsed: new(float64(1000))}, func() { close(entered); <-release })
				})
			}()
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("state commit did not reach the real staged file")
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			unblock()
			var err error
			select {
			case err = <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("state commit did not finish")
			}
			after, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) {
					t.Fatalf("stopped state committed: %s %v", after, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				var original, edited map[string]json.RawMessage
				if err := json.Unmarshal(before, &original); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(after, &edited); err != nil {
					t.Fatal(err)
				}
				// The error text is kept (written in Go's encoding); an
				// undeclared member is dropped (O18 T1d).
				var before, now string
				if json.Unmarshal(original["error"], &before) != nil || json.Unmarshal(edited["error"], &now) != nil || before != now {
					t.Fatalf("error text changed: %s -> %s", original["error"], edited["error"])
				}
				if _, kept := edited["future"]; kept {
					t.Fatal("undeclared state member kept")
				}
				state, err := owner.loadState()
				if err != nil || state.State == nil || *state.State != "running" || state.LastUsed == nil || *state.LastUsed != 1000 {
					t.Fatalf("state not committed: %+v %v", state, err)
				}
			}
			if leftovers, err := filepath.Glob(filepath.Join(root, ".state.*.tmp")); err != nil || len(leftovers) > 0 {
				t.Fatalf("staged state leaked: %v %v", leftovers, err)
			}
		})
	}
}
