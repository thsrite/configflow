package jsonstore_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/jsonstore"
	"configflow/internal/lifecycle"
)

func ownerLifetime(t *testing.T, shutdown bool) (context.Context, func()) {
	t.Helper()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	r.Start(context.Background())
	t.Cleanup(func() { r.Shutdown(context.Background()) })
	request, disconnect := context.WithCancel(r.BindRequest(context.Background()))
	t.Cleanup(disconnect)
	if shutdown {
		return lifecycle.Detach(request), func() { r.Shutdown(context.Background()) }
	}
	return lifecycle.Detach(request), disconnect
}

func TestContextWritePublicationLifetime(t *testing.T) {
	for _, signed := range []bool{false, true} {
		for _, stage := range []string{"temp-written", "before-rename"} {
			for _, shutdown := range []bool{true, false} {
				t.Run(fmt.Sprintf("signed=%v/%s/shutdown=%v", signed, stage, shutdown), func(t *testing.T) {
					ctx, cancelOwner := ownerLifetime(t, shutdown)
					s := jsonstore.New(t.TempDir())
					const before = `{"value":"old"}`
					const after = `{"value":"new"}`
					path := s.Path("synthetic")
					for _, name := range []string{path, path + ".bak"} {
						if err := os.WriteFile(name, []byte(before), 0600); err != nil {
							t.Fatal(err)
						}
					}
					oldSignature, err := jsonstore.FileSignatureForTest(path)
					if err != nil {
						t.Fatal(err)
					}
					paused := false
					barrier := func() {
						paused = true
						if got, err := os.ReadFile(path + ".tmp"); err != nil || string(got) != after {
							t.Fatalf("barrier missed completed temp write: %s %v", got, err)
						}
						cancelOwner()
					}
					if stage == "temp-written" {
						jsonstore.SetPublicationHooksForTest(s, barrier, nil, nil)
					} else {
						jsonstore.SetPublicationHooksForTest(s, nil, barrier, nil)
					}
					sig, err := s.WriteContext(ctx, "synthetic", struct {
						Value string `json:"value"`
					}{"new"}, signed)
					if !paused {
						t.Fatal("publication barrier was not reached")
					}
					if shutdown {
						if !errors.Is(err, context.Canceled) || sig != nil {
							t.Errorf("shutdown write: signature=%v error=%v", sig, err)
						}
						if got, err := jsonstore.FileSignatureForTest(path); err != nil || got != oldSignature {
							t.Errorf("shutdown changed signature: %v %v", got, err)
						}
					} else if err != nil || (signed && sig == nil) || (!signed && sig != nil) {
						t.Errorf("disconnected write: signature=%v error=%v", sig, err)
					}
					want := after
					if shutdown {
						want = before
					}
					if got, err := os.ReadFile(path); err != nil || string(got) != want {
						t.Errorf("main file: %s %v", got, err)
					}
					if got, err := os.ReadFile(path + ".bak"); err != nil || string(got) != before {
						t.Errorf("backup changed: %s %v", got, err)
					}
					if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
						t.Errorf("temp remains: %v", err)
					}
				})
			}
		}
	}
}

func TestContextRemovePublicationLifetime(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		t.Run(fmt.Sprintf("shutdown=%v", shutdown), func(t *testing.T) {
			ctx, cancelOwner := ownerLifetime(t, shutdown)
			s := jsonstore.New(t.TempDir())
			path := s.Path("synthetic")
			const before = `{"value":"old"}`
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			oldSignature, err := jsonstore.FileSignatureForTest(path)
			if err != nil {
				t.Fatal(err)
			}
			paused := false
			jsonstore.SetPublicationHooksForTest(s, nil, func() { paused = true; cancelOwner() }, nil)
			err = s.RemoveContext(ctx, "synthetic")
			if !paused {
				t.Fatal("unlink barrier was not reached")
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("shutdown remove: %v", err)
				}
				if got, err := os.ReadFile(path); err != nil || string(got) != before {
					t.Errorf("main file: %s %v", got, err)
				}
				if got, err := jsonstore.FileSignatureForTest(path); err != nil || got != oldSignature {
					t.Errorf("signature changed: %v %v", got, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("disconnect prevented unlink: %v", err)
				}
			}
		})
	}
}

func TestContextSignedWriteCompletesVerificationAfterRename(t *testing.T) {
	ctx, shutdown := ownerLifetime(t, true)
	s := jsonstore.New(t.TempDir())
	committed := false
	jsonstore.SetPublicationHooksForTest(s, nil, nil, func(string) { committed = true; shutdown() })
	sig, err := s.WriteContext(ctx, "synthetic", struct {
		Value int `json:"value"`
	}{1}, true)
	if !committed || err != nil || sig == nil {
		t.Fatalf("committed=%v signature=%v error=%v", committed, sig, err)
	}
	if got, err := jsonstore.FileSignatureForTest(s.Path("synthetic")); err != nil || *sig != got {
		t.Fatalf("signature: %v %v", got, err)
	}
}

func TestContextAlreadyStoppedDoesNotTouchDirectory(t *testing.T) {
	ctx, shutdown := ownerLifetime(t, true)
	shutdown()
	dir := filepath.Join(t.TempDir(), "absent")
	s := jsonstore.New(dir)
	if sig, err := s.WriteContext(ctx, "synthetic", struct{}{}, false); !errors.Is(err, context.Canceled) || sig != nil {
		t.Errorf("write: %v %v", sig, err)
	}
	if err := s.RemoveContext(ctx, "synthetic"); !errors.Is(err, context.Canceled) {
		t.Errorf("remove: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stopped write created directory: %v", err)
	}
}

func TestContextWriteFailureCleansTempFile(t *testing.T) {
	for _, signed := range []bool{false, true} {
		t.Run(fmt.Sprintf("signed=%v", signed), func(t *testing.T) {
			s := jsonstore.New(t.TempDir())
			if err := os.Mkdir(s.Path("synthetic"), 0700); err != nil {
				t.Fatal(err)
			}
			if sig, err := s.WriteContext(context.Background(), "synthetic", struct{}{}, signed); err == nil || sig != nil {
				t.Fatalf("write: %v %v", sig, err)
			}
			if _, err := os.Stat(s.Path("synthetic") + ".tmp"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed rename left temp: %v", err)
			}
			if info, err := os.Stat(s.Path("synthetic")); err != nil || !info.IsDir() {
				t.Errorf("destination changed: %v %v", info, err)
			}
		})
	}
}

// Capture nil before stopping the real owner, modelling preemption after the
// last cancellation read. This is the narrow, unavoidable check/syscall window;
// unlike a beforeCommit hook, it deliberately admits the current operation.
type reviewFinalCheckContext struct {
	context.Context
	checks int
	pause  func()
}

func (c *reviewFinalCheckContext) Err() error {
	err := c.Context.Err()
	c.checks++
	if c.checks == 2 {
		c.pause()
	}
	return err
}

func TestReviewCancellationAfterFinalCheckAdmitsOneCommit(t *testing.T) {
	for _, operation := range []string{"unsigned", "signed", "remove"} {
		t.Run(operation, func(t *testing.T) {
			owner, shutdown := ownerLifetime(t, true)
			s := jsonstore.New(t.TempDir())
			path := s.Path("synthetic")
			const before, after = `{"value":"old"}`, `{"value":"new"}`
			if err := os.WriteFile(path, []byte(before), 0600); err != nil {
				t.Fatal(err)
			}
			oldSignature, err := jsonstore.FileSignatureForTest(path)
			if err != nil {
				t.Fatal(err)
			}
			paused := false
			ctx := &reviewFinalCheckContext{Context: owner, pause: func() {
				paused = true
				if got, err := os.ReadFile(path); err != nil || string(got) != before {
					t.Fatalf("check was after commit: %q %v", got, err)
				}
				if operation != "remove" {
					if got, err := os.ReadFile(path + ".tmp"); err != nil || string(got) != after {
						t.Fatalf("check was before temp write: %q %v", got, err)
					}
				}
				shutdown()
			}}
			var sig *jsonstore.Signature
			if operation == "remove" {
				err = s.RemoveContext(ctx, "synthetic")
			} else {
				sig, err = s.WriteContext(ctx, "synthetic", struct {
					Value string `json:"value"`
				}{"new"}, operation == "signed")
			}
			if !paused || err != nil || !errors.Is(owner.Err(), context.Canceled) {
				t.Fatalf("paused=%v error=%v owner=%v", paused, err, owner.Err())
			}
			got, readErr := os.ReadFile(path)
			if operation == "remove" {
				if !errors.Is(readErr, os.ErrNotExist) {
					t.Fatalf("unlink not admitted: %v", readErr)
				}
			} else if readErr != nil || string(got) != after {
				t.Fatalf("rename not admitted: %q %v", got, readErr)
			}
			if operation == "signed" {
				current, err := jsonstore.FileSignatureForTest(path)
				if err != nil || sig == nil || *sig != current || *sig == oldSignature {
					t.Fatalf("stale signature: got=%v current=%v old=%v error=%v", sig, current, oldSignature, err)
				}
			}
			if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("temp remains: %v", err)
			}
			// The admitted operation does not grant permission to any later one.
			if err := s.RemoveContext(owner, "synthetic"); !errors.Is(err, context.Canceled) {
				t.Errorf("next operation admitted: %v", err)
			}
		})
	}
}
