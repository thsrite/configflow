package deploy

import (
	"context"
	"errors"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func TestServicePublishFailsClosedWithoutPairPublisher(t *testing.T) {
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		enabled := true
		s.Agents = []model.Agent{{
			ID:          "ha-oxidns-1",
			Name:        "HA OxiDNS",
			ProfileID:   "default",
			ServiceType: "oxidns",
			Host:        "192.0.2.10",
			Enabled:     &enabled,
			OxiDNSHA: &model.HASettings{
				Enabled: &enabled,
				GroupID: "pair-1",
				PeerID:  "ha-oxidns-2",
				Role:    "primary",
			},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	m, err := agents.New(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	svc := New(Options{})
	_, err = svc.Publish(ctx, m, "ha-oxidns-1", &Bundle{}, PublishOptions{})
	if !errors.Is(err, ErrPairPublisherMissing) {
		t.Fatalf("Publish without PairPublisher = %v, want %v", err, ErrPairPublisherMissing)
	}
}
