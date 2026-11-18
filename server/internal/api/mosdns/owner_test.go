package mosdns

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestTypedSettingsRuntimeOwnership(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "disconnect", true: "shutdown"}[shutdown], func(t *testing.T) {
			repository, err := repo.Open(t.TempDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			root := repo.WithScope(repo.WithRepository(context.Background(), repository))
			runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: time.Second})
			runtime.Start(root)
			t.Cleanup(func() { runtime.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(runtime.BindRequest(root))
			defer cancel()
			before, err := repository.ConfigRevision(root, "default")
			if err != nil {
				t.Fatal(err)
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				cancel()
			}
			value := model.MosDNSSettings{CustomHosts: "192.0.2.4 saved.example"}
			_, err = save(request, "dns-servers", value)
			after, readErr := repository.ConfigRevision(root, "default")
			if readErr != nil {
				t.Fatal(readErr)
			}
			current, readErr := repository.Profile(root, "default")
			if readErr != nil {
				t.Fatal(readErr)
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) || after != before || current.MosDNS.CustomHosts == value.CustomHosts {
					t.Fatalf("shutdown committed: %v %d -> %d", err, before, after)
				}
			} else if err != nil || after != before+1 || current.MosDNS.CustomHosts != value.CustomHosts {
				t.Fatalf("disconnect lost owned write: %v", err)
			}
		})
	}
}
func TestTypedSettingsShutdownWhileWaitingForTransaction(t *testing.T) {
	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	root := repo.WithScope(repo.WithRepository(context.Background(), repository))
	runtime := lifecycle.NewRuntime(lifecycle.Options{Grace: time.Second})
	runtime.Start(root)
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	request := runtime.BindRequest(root)
	locked, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		_, err := repository.UpdateProfile(root, "default", func(context.Context, *model.Profile) error { close(locked); <-release; return nil })
		holder <- err
	}()
	<-locked
	result := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		_, err := save(request, "dns-servers", model.MosDNSSettings{CustomHosts: "192.0.2.8 blocked.example"})
		result <- err
	}()
	<-started
	runtime.Shutdown(context.Background())
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued write survived shutdown: %v", err)
	}
	current, err := repository.Profile(root, "default")
	if err != nil {
		t.Fatal(err)
	}
	if current.MosDNS.CustomHosts == "192.0.2.8 blocked.example" {
		t.Fatal("queued write committed")
	}
}
