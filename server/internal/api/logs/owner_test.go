package logs

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

func TestClearOwnerRuntimeAndFinalCommitCheck(t *testing.T) {
	for _, mode := range []string{"disconnect", "shutdown", "shutdown after lookup"} {
		t.Run(mode, func(t *testing.T) {
			c := newLogClient(t)
			before := []byte("synthetic log retained until commit\n")
			if err := os.WriteFile(c.path, before, 0600); err != nil {
				t.Fatal(err)
			}
			r := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Millisecond})
			r.Start(context.Background())
			t.Cleanup(func() { r.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(r.BindRequest(context.Background()))
			defer cancel()
			if mode == "shutdown" {
				r.Shutdown(context.Background())
			} else {
				cancel()
			}
			if mode == "shutdown after lookup" {
				LogFilePath = func() (string, error) { r.Shutdown(context.Background()); return c.path, nil }
			}
			_, err := clearLog(request)
			after, readErr := os.ReadFile(c.path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if mode == "disconnect" {
				if err != nil || len(after) != 0 {
					t.Fatal("disconnect aborted clear", err)
				}
			} else if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) {
				t.Fatal("shutdown truncated log", err)
			}
		})
	}
}
