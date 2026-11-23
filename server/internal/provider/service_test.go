package provider

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/model"
	"configflow/internal/refresh"
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
