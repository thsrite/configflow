package provider

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/substore"
	"configflow/internal/yamlx"
)

func TestServiceFailClosedWhenAggregationsUnset(t *testing.T) {
	svc := New(Options{})
	if err := svc.Validate(); !errors.Is(err, ErrAggregationProviderMissing) {
		t.Fatalf("Validate() = %v, want %v", err, ErrAggregationProviderMissing)
	}
	_, err := svc.AggregationRenderer()(context.Background(), model.Aggregation{}, &model.Config{}, nil, nil, nil)
	if !errors.Is(err, ErrAggregationProviderMissing) {
		t.Fatalf("nil aggregation renderer = %v, want %v", err, ErrAggregationProviderMissing)
	}
}

func TestServiceSetAggregationsTwoPhaseEnforcement(t *testing.T) {
	svc := New(Options{})
	renderer := func(context.Context, model.Aggregation, *model.Config, *yamlx.Node, *DeliverySnapshot, refresh.FetchSourceFn) (*Artifact, error) {
		return &Artifact{Name: "ok"}, nil
	}
	if err := svc.SetAggregations(renderer); err != nil {
		t.Fatalf("first SetAggregations failed: %v", err)
	}
	if err := svc.Validate(); err != nil {
		t.Fatalf("Validate() after SetAggregations = %v, want nil", err)
	}
	if err := svc.SetAggregations(renderer); !errors.Is(err, ErrAggregationsAlreadySet) {
		t.Fatalf("second SetAggregations = %v, want %v", err, ErrAggregationsAlreadySet)
	}

	started := New(Options{})
	started.MarkStarted()
	if err := started.SetAggregations(renderer); !errors.Is(err, ErrServiceStarted) {
		t.Fatalf("SetAggregations after MarkStarted = %v, want %v", err, ErrServiceStarted)
	}
}

// A Service bound to an explicit Sub-Store keeps the default environment's
// optional conversion: a node Sub-Store cannot convert is skipped, never a
// delivery failure (review W0–W8).
func TestServiceDeliveryConverterSkipsFailedConversions(t *testing.T) {
	env := substore.NewEnv()
	env.DataDir = t.TempDir
	env.LookupEnv = func(key string) (string, bool) {
		switch key {
		case "SUB_STORE_URL":
			return "http://127.0.0.1:1", true
		case "SUB_STORE_BUILTIN_DIR":
			return t.TempDir(), true
		}
		return "", false
	}
	sub := substore.NewService(env)
	ctx := context.Background()
	const line = "vmess://synthetic-unconvertible"
	if _, err := sub.Client.ConvertProxy(ctx, line); err == nil {
		t.Fatal("control: the unreachable Sub-Store client must fail")
	}
	node, err := New(Options{SubStore: sub}).DeliveryEnv().converter().ConvertProxy(ctx, line)
	if err != nil || node != nil {
		t.Fatalf("delivery converter = (%v, %v), want a skipped node", node, err)
	}
}
