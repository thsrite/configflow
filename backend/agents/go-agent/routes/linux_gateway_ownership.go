package routes

// nft 1.0.6 accepts table/chain comments in JSON but silently omits them. Rule
// comments survive. Only the complete, token-tagged rule plan can prove that an
// unpinned first transaction created that otherwise unlabelled table. Once its
// read-back is journaled, metadata and kernel handles must match exactly again.
func gatewayNFTObjectCommentsMissing(objects []map[string]any) bool {
	count := 0
	for _, object := range objects {
		for kind, value := range object {
			if kind != "table" && kind != "chain" {
				continue
			}
			body, ok := value.(map[string]any)
			if !ok {
				return false
			}
			if _, exists := body["comment"]; exists {
				return false
			}
			count++
		}
	}
	return count == 3
}

func gatewayNFTWithoutObjectComments(objects []map[string]any) []map[string]any {
	result := make([]map[string]any, len(objects))
	for i, object := range objects {
		result[i] = canonicalGatewayNFT(object).(map[string]any)
		for _, kind := range []string{"table", "chain"} {
			if body, ok := result[i][kind].(map[string]any); ok {
				delete(body, "comment")
			}
		}
	}
	return result
}

func gatewayNFTHandles(objects []map[string]any, present bool) bool {
	for _, object := range objects {
		if len(object) != 1 {
			return false
		}
		for _, value := range object {
			body, ok := value.(map[string]any)
			if !ok {
				return false
			}
			handle, exists := body["handle"]
			if present {
				if number, ok := resourceNumber(handle); !exists || !ok || number == 0 {
					return false
				}
			} else if exists {
				return false
			}
		}
	}
	return len(objects) != 0
}

// Headers may be serialized in a different order, but rules within each chain
// are executable order. Comparing only an unordered object inventory would
// incorrectly accept an externally reordered chain or a reordered journal.
func gatewayNFTListsEqual(expected, actual []map[string]any, equal func(map[string]any, map[string]any) bool) bool {
	if len(expected) != len(actual) {
		return false
	}
	used := make([]bool, len(actual))
	for _, planned := range expected {
		found := false
		for i, object := range actual {
			if !used[i] && equal(planned, object) {
				used[i], found = true, true
				break
			}
		}
		if !found {
			return false
		}
	}
	rules := func(objects []map[string]any) (map[string][]map[string]any, bool) {
		chains := map[string][]map[string]any{}
		for _, object := range objects {
			value, exists := object["rule"]
			if !exists {
				continue
			}
			body, ok := value.(map[string]any)
			if !ok {
				return nil, false
			}
			family, familyOK := body["family"].(string)
			table, tableOK := body["table"].(string)
			chain, chainOK := body["chain"].(string)
			if !familyOK || !tableOK || !chainOK {
				return nil, false
			}
			key := family + "/" + table + "/" + chain
			chains[key] = append(chains[key], object)
		}
		return chains, true
	}
	plannedRules, plannedOK := rules(expected)
	actualRules, actualOK := rules(actual)
	if !plannedOK || !actualOK || len(plannedRules) != len(actualRules) {
		return false
	}
	for chain, planned := range plannedRules {
		current := actualRules[chain]
		if len(planned) != len(current) {
			return false
		}
		for i, rule := range planned {
			if !equal(rule, current[i]) {
				return false
			}
		}
	}
	return true
}

func gatewayJournalNFTValid(state *gatewayState) bool {
	expected := gatewayDesiredNFT(state)
	if gatewayNFTObjectCommentsMissing(state.NFT) {
		// Missing metadata is a captured kernel representation, never an
		// arbitrary comment-free creation plan without pinned identities.
		if !gatewayNFTHandles(state.NFT, true) {
			return false
		}
		expected = gatewayNFTWithoutObjectComments(expected)
	}
	return gatewayNFTListsEqual(expected, state.NFT, gatewayNormalizedResourceEqual)
}

func gatewayInitialNFTReadback(state *gatewayState, actual []map[string]any) bool {
	return state.Pending && !state.Active && state.AppliedAt == "" &&
		gatewayNFTHandles(state.NFT, false) && gatewayNFTHandles(actual, true) &&
		gatewayNFTObjectCommentsMissing(actual) &&
		gatewayNFTListsEqual(gatewayDesiredNFT(state), state.NFT, gatewayNormalizedResourceEqual)
}
