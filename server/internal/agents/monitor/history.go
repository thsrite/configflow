package monitor

// The parts of backend/agents/oxidns_ranking_history.py the overview reads
// (constants, supported, get_history(...).query / .has_any). The store, the
// collector loop and its SQLite schema are A07's: it registers the real
// store with SetRankingHistory. Until then the default store answers like
// Python's when metrics/oxidns_rankings.sqlite3 does not exist (nothing
// stored) and fails otherwise, so existing history is never hidden silently.

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

// Constants of oxidns_ranking_history.py used here.
const (
	HistoryAgentVersion = "1.8.55-go"
	HistoryFilename     = "oxidns_rankings.sqlite3"
	HistorySlot         = 900
	HistoryHour         = 3600
	HistoryDay          = 86400
	HistoryRetentionDay = 90
)

// HistoryRanges is RANGES (seconds per range key).
var HistoryRanges = map[string]int64{"24h": HistoryDay, "7d": 7 * HistoryDay, "30d": 30 * HistoryDay}

// HistorySupported is supported(agent).
func HistorySupported(agent *jsonx.Object) bool {
	version, _ := agent.Value("version").(string)
	if version == "" {
		version = "0"
	}
	return agents.CompareVersions(version, HistoryAgentVersion) >= 0
}

// HistoryRow is one {'key', 'count'} of a stored ranking.
type HistoryRow struct {
	Key   string
	Count int64
}

// HistoryAnswer is RankingHistory.query's result. First and Last are
// meaningful only when Slots > 0.
type HistoryAnswer struct {
	Records, BlockedRecords, BlockedSlots, Slots, PartialSlots int64
	First, Last                                                int64
	Rows                                                       map[string][]HistoryRow // clients, domains, blocked
}

// RankingHistory is the stored complete rankings (get_history(data_dir)).
type RankingHistory interface {
	HasAny(agentID, recorder string) (bool, error)
	Query(agentID, recorder string, since, until int64) (*HistoryAnswer, error)
}

// ErrHistoryNotPorted is the default store's answer when a ranking history
// database exists (A07 replaces the store).
var ErrHistoryNotPorted = errors.New("oxidns ranking history store is not ported yet (A07)")

type absentHistory struct{ path string }

func (h absentHistory) exists() (bool, error) {
	_, err := os.Stat(h.path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return true, nil
}

func (h absentHistory) HasAny(string, string) (bool, error) {
	if ok, _ := h.exists(); ok {
		return false, ErrHistoryNotPorted
	}
	return false, nil
}

func (h absentHistory) Query(string, string, int64, int64) (*HistoryAnswer, error) {
	if ok, _ := h.exists(); ok {
		return nil, ErrHistoryNotPorted
	}
	return &HistoryAnswer{Rows: map[string][]HistoryRow{"clients": nil, "domains": nil, "blocked": nil}}, nil
}

var (
	historyMu      sync.RWMutex
	historyFactory = func(dataDir string) RankingHistory {
		return absentHistory{filepath.Join(dataDir, "metrics", HistoryFilename)}
	}
)

// SetRankingHistory installs get_history (A07) and returns a function
// restoring the previous one.
func SetRankingHistory(factory func(dataDir string) RankingHistory) (restore func()) {
	historyMu.Lock()
	previous := historyFactory
	historyFactory = factory
	historyMu.Unlock()
	return func() {
		historyMu.Lock()
		historyFactory = previous
		historyMu.Unlock()
	}
}

func getHistory(dataDir string) RankingHistory {
	historyMu.RLock()
	defer historyMu.RUnlock()
	return historyFactory(dataDir)
}
