package substore

import (
	"bytes"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
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
)

func nativeRuntimeReference(t *testing.T) *jsonx.Object {
	t.Helper()
	data, err := os.ReadFile("testdata/n89_runtime_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/review_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	fixture := value.(*jsonx.Object)
	digest := sha256.Sum256(source)
	if fixture.GetString("source_sha256") != hex.EncodeToString(digest[:]) {
		t.Fatal("runtime reference source changed")
	}
	if len(fixture.GetList("expectations")) != len(reviewReference(t).GetList("runtime")) {
		t.Fatal("runtime reference coverage changed")
	}
	return fixture
}
func compareRuntimeStateFile(t *testing.T, label string, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	actual, e1 := jsonx.Parse(got)
	expected, e2 := jsonx.Parse(want)
	if e1 != nil || e2 != nil || !jsonx.Equal(actual, expected) {
		t.Errorf("%s: state.json %q want %q", label, got, want)
		return
	}
	a, ok1 := actual.(*jsonx.Object)
	b, ok2 := expected.(*jsonx.Object)
	if ok1 && ok2 && !slices.Equal(a.Keys(), b.Keys()) {
		t.Errorf("%s: state member order changed: %v want %v", label, a.Keys(), b.Keys())
	}
}
func TestN89RuntimeReadsOriginalPythonBytes(t *testing.T) {
	fixture := nativeRuntimeReference(t)
	data := []byte(fixture.GetString("old_python_bytes"))
	if !bytes.Contains(data, []byte(`\u`)) || !bytes.Contains(data, []byte(": ")) {
		t.Fatal("fixture is not original Python framing")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	runtime := NewRuntime(&Env{}, root, 300)
	state, err := runtime.loadState()
	if err != nil || state.State == nil || *state.State != "failed" || state.Error != "旧状态" || state.LastUsed != 123.5 {
		t.Fatalf("old state not read: %+v %v", state, err)
	}
	unknown, _ := state.Fields.Get("future")
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(unknown, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["number"]) != "9007199254740993123" {
		t.Fatal("unknown exact number lost")
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
			before := []byte(nativeRuntimeReference(t).GetString("old_python_bytes"))
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
				for _, key := range []string{"error", "future"} {
					if !bytes.Equal(original[key], edited[key]) {
						t.Fatalf("unowned state member changed: %s", key)
					}
				}
				state, err := owner.loadState()
				if err != nil || state.State == nil || *state.State != "running" || state.LastUsed != 1000 {
					t.Fatalf("state not committed: %+v %v", state, err)
				}
			}
			if leftovers, err := filepath.Glob(filepath.Join(root, ".state.*.tmp")); err != nil || len(leftovers) > 0 {
				t.Fatalf("staged state leaked: %v %v", leftovers, err)
			}
		})
	}
}
