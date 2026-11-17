// R08 落地后移除。The approved R07 clarification keeps this read-only bridge
// until *ROSPolicyReplace exposes typed PrepareStatic []Op. No device operation
// or plan is built from JSON here; only nested migration counts are read.
// TODO(R08): replace nestedNATMigration with typed traversal and delete this file.

package routers

import "encoding/json"

func nestedNATMigration(op Op) (merged, stale int) {
	if op.OpKind() != "ros_policy_replace" {
		return
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return
	}
	var count func(json.RawMessage)
	count = func(raw json.RawMessage) {
		var value struct {
			Kind    string            `json:"kind"`
			Prepare []json.RawMessage `json:"prepare_static"`
			Legacy  []ROSResource     `json:"legacy"`
			Stale   []ROSResource     `json:"stale"`
		}
		if json.Unmarshal(raw, &value) != nil {
			return
		}
		if value.Prepare != nil {
			for _, inner := range value.Prepare {
				count(inner)
			}
			return
		}
		if value.Kind == "nat_list_sync" {
			merged += len(value.Legacy)
			stale += len(value.Stale)
		}
	}
	count(raw)
	return
}
