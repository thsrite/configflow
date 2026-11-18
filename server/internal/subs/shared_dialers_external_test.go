package subs_test

// Exercise the production registration, without importing convert from subs.
import (
	"bytes"
	_ "configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"context"
	"errors"
	"testing"
)

func TestN89AddGroupsRuntimeStopsBeforeGraphCommit(t *testing.T) {
	main := yamlx.Map(yamlx.Str("proxy-providers"), yamlx.Map(yamlx.Str("Air"), yamlx.Map(yamlx.Str("url"), yamlx.Str("https://panel.example/api/subscriptions/source/proxies"))))
	config := &model.Config{Profile: &model.Profile{}, Subscriptions: []model.Subscription{{ID: "source", Name: "Air"}}}
	before, err := yamlx.ToJSON(main)
	if err != nil {
		t.Fatal(err)
	}
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(context.Background())
	defer runtime.Shutdown(context.Background())
	ctx := lifecycle.Detach(runtime.BindRequest(context.Background()))
	converted := 0
	_, err = subs.AddGroups(ctx, main, config, subs.DialerInputs{LoadCache: func(context.Context, string) (*subs.SubscriptionCache, error) {
		runtime.Shutdown(context.Background())
		return &subs.SubscriptionCache{}, nil
	}, Convert: func(context.Context, model.Node) (*yamlx.Node, error) { converted++; return nil, nil }})
	after, encodeErr := yamlx.ToJSON(main)
	if !errors.Is(err, context.Canceled) || converted != 0 || encodeErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("shutdown continued converted=%d err=%v", converted, err)
	}
}
