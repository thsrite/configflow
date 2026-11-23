package provider

import (
	"testing"

	"configflow/internal/store"
)

func TestO17FollowupFlightKeyIgnoresStateGeneration(t *testing.T) {
	h1 := &store.Head{StoreID: "s", Generation: 3, CommitID: "c3", State: 1}
	h2 := &store.Head{StoreID: "s", Generation: 3, CommitID: "c3", State: 9}
	h3 := &store.Head{StoreID: "s", Generation: 4, CommitID: "c4", State: 9}

	k1 := newFlightKey("aggregation", "default", "a1", "", "", "", h1)
	k2 := newFlightKey("aggregation", "default", "a1", "", "", "", h2)
	k3 := newFlightKey("aggregation", "default", "a1", "", "", "", h3)

	if k1 != k2 {
		t.Fatalf("flightKey differed across state-only change: %+v vs %+v", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("flightKey matched across config change: %+v vs %+v", k1, k3)
	}
}
