package mihomoenv

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

func TestN89NativeEnvironmentPreservesShutdownAndCacheRecords(t *testing.T) {
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	bound := runtime.BindRequest(context.Background())
	runtime.Shutdown(context.Background())
	repository, err := repo.OpenDefault(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sub := substore.NewService(substore.NewEnvFor(repository))
	env := NativeEnvFor(repository, sub, nil)
	if node, err := env.Converter.ConvertProxy(bound, "ss://synthetic"); node != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("native conversion lost Runtime shutdown: %v", err)
	}
	ctx := context.Background()
	if _, err := substore.SaveCache(ctx, repository, "source", []model.Node{{Name: "Node", Type: "http", Server: "192.0.2.1", Port: model.IntPtr(443)}}, nil); err != nil {
		t.Fatal(err)
	}
	data, err := env.LoadSubscriptionCache(ctx, "source")
	if err != nil {
		t.Fatal(err)
	}
	var cache substore.Cache
	if err := json.Unmarshal(data, &cache); err != nil || len(cache.Nodes) != 1 || model.IntValue(cache.Nodes[0].Port) != 443 || cache.Nodes[0].Name != "Node" {
		t.Fatalf("native cache %s %v", data, err)
	}
}
