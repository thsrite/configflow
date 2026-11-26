package routes

import (
	"fmt"
	"strings"
)

// An unfinished transaction (Pending) may have written nft objects that never
// reached the journal, e.g. Agents before 1.9.1 on nft 1.0.6 failed the
// readback of their own table/chain comments and stopped right there. The
// journal token is 128 random bits known only to this state file, so objects
// that carry it were written by this journal and can be recorded again.

// pendingReturnRefusal is returned when token evidence cannot cover every
// unrecorded object of the managed table.
const pendingReturnRefusal = "上次回流事务未完成，且 nft 表 %s 中存在无法用本机归属标识证明的对象，保留现有规则，请先人工核对"

// pendingNFTManifest returns the recorded nft objects plus every unrecorded
// object in the managed table that carries owner's token: chains named
// cf_<token>_*, rules in those chains whose comment starts with the owner tag,
// and the table itself when it holds such objects (or carries the tag). Any
// other unrecorded object makes the state unrecoverable.
func pendingNFTManifest(owner *returnOwnership, resources *returnResources) ([]map[string]any, error) {
	refusal := fmt.Errorf(pendingReturnRefusal, managedReturnTable)
	manifest := append([]map[string]any{}, owner.NFT...)
	used := make([]bool, len(owner.NFT))
	prefix, tag := "cf_"+owner.Token+"_", ownerTag(owner)
	var table map[string]any
	tableTagged, tokenObjects := false, 0
	for _, object := range ownedNftObjects(resources) {
		if consumeOwned(object, owner.NFT, used) {
			continue
		}
		if len(object) != 1 {
			return nil, refusal
		}
		for kind, raw := range object {
			body, _ := raw.(map[string]any)
			comment, hasComment := body["comment"].(string)
			if _, present := body["comment"]; present && !hasComment {
				return nil, refusal
			}
			switch kind {
			case "table":
				if table != nil || (hasComment && comment != tag) {
					return nil, refusal
				}
				table, tableTagged = object, hasComment
			case "chain":
				name, _ := body["name"].(string)
				if !strings.HasPrefix(name, prefix) || (hasComment && comment != tag) {
					return nil, refusal
				}
				manifest = append(manifest, object)
				tokenObjects++
			case "rule":
				chain, _ := body["chain"].(string)
				if !strings.HasPrefix(chain, prefix) || !strings.HasPrefix(comment, tag+":") {
					return nil, refusal
				}
				manifest = append(manifest, object)
				tokenObjects++
			default:
				return nil, refusal
			}
		}
	}
	if table != nil {
		// An untagged table with nothing of ours in it proves nothing.
		if !tableTagged && tokenObjects == 0 {
			return nil, refusal
		}
		manifest = append(manifest, table)
	}
	return manifest, nil
}

// recoverPendingReturn records the token-proven nft objects of an unfinished
// transaction so that Apply and Remove can replay or undo it exactly.
func recoverPendingReturn(cfg *Config, state *returnRoutingState) error {
	resources, err := readReturnResources()
	if err != nil {
		return err
	}
	manifest, err := pendingNFTManifest(state.Ownership, resources)
	if err != nil {
		return err
	}
	if len(manifest) == len(state.Ownership.NFT) {
		return nil
	}
	state.Ownership.NFT = manifest
	if !validReturnOwner(state) {
		return fmt.Errorf(pendingReturnRefusal, managedReturnTable)
	}
	return saveReturnState(cfg, state)
}
