package monitor

import ()

// ResetCoreLatest empties _core_latest (tests).
func ResetCoreLatest() {
	coreLatestMu.Lock()
	coreLatest = map[string]*coreCached{}
	coreLatestMu.Unlock()
}

// ResetRankingStores forgets every store (tests: a fresh process).
func ResetRankingStores() {
	storesMu.Lock()
	stores = map[string]*RankingStore{}
	storesMu.Unlock()
}
