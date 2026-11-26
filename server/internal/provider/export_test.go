package provider

import (
	"strconv"
	"sync"
)

// ValidateArtifacts checks exact prepared content and all provider URL aliases.
func ValidateArtifacts(snapshot *DeliverySnapshot, providers []*Artifact) (bool, error) {
	return validateArtifactsWith(PassthroughLocations{}, snapshot, providers)
}

// SequentialNodeIDs returns a NodeID generator node_00000001, node_00000002,
// ... (tests and reference corpora).
func SequentialNodeIDs() func() string {
	var mu sync.Mutex
	n := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		n++
		s := strconv.FormatInt(int64(n), 16)
		for len(s) < 8 {
			s = "0" + s
		}
		return "node_" + s
	}
}
