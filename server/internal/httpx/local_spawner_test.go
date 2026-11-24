package httpx_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/localhttp"
)

func TestRegisterLocalUsesDefaultSpawner(t *testing.T) {
	// Isolate the production default Runtime from other HTTP fixture generations.
	if os.Getenv("O37_LOCAL_SPAWNER_CHILD") != "1" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestRegisterLocalUsesDefaultSpawner$", "-test.count=1")
		cmd.Env = append(os.Environ(), "O37_LOCAL_SPAWNER_CHILD=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("child: %v\n%s", err, output)
		}
		return
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(context.Background())
	defer rt.Stop(time.Second)
	app := httpx.New()
	app.Runtime = rt
	entered, release := make(chan struct{}), make(chan struct{})
	origin := make(chan *lifecycle.Runtime, 1)
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	app.InsertBeforeRequest(0, func(c *httpx.Ctx) (*httpx.Response, error) {
		rt, _ := lifecycle.SpawnerFrom(c.Context()).(*lifecycle.Runtime)
		origin <- rt
		close(entered)
		<-release
		return httpx.Text(200, "synthetic local callback"), nil
	})
	app.RegisterLocal()
	defer localhttp.Register(nil, nil)
	done := make(chan error, 1)
	go func() {
		response, err := localhttp.Do(t.Context(), localhttp.Request{URL: "http://example.com/api/synthetic"})
		if response != nil {
			_, readErr := response.Content()
			err = errors.Join(err, readErr)
		}
		done <- err
	}()
	<-entered
	got := <-origin
	if got == nil {
		t.Fatal("production local callback is not inside a Runtime job")
	}
	waiting, cancel := context.WithCancel(t.Context())
	cancel()
	if err := got.WaitIdle(waiting); !errors.Is(err, context.Canceled) {
		t.Fatal("production RegisterLocal did not inject default Spawner", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := got.WaitIdle(t.Context()); err != nil {
		t.Fatal(err)
	}
}
