package monitor

// The parts of backend/agents/oxidns_ranking_history.py the overview reads
// (constants, supported, get_history(...).query / .has_any).

import (
	"context"
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

func getHistory(dataDir string) RankingHistory {
	return rankingStoreFactory(dataDir)
}

func queryHistory(ctx context.Context, history RankingHistory, id, recorder string, since, until int64) (*HistoryAnswer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if native, ok := history.(interface {
		QueryContext(context.Context, string, string, int64, int64) (*HistoryAnswer, error)
	}); ok {
		return native.QueryContext(ctx, id, recorder, since, until)
	}
	return history.Query(id, recorder, since, until)
}
func hasHistory(ctx context.Context, history RankingHistory, id, recorder string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if native, ok := history.(interface {
		HasAnyContext(context.Context, string, string) (bool, error)
	}); ok {
		return native.HasAnyContext(ctx, id, recorder)
	}
	return history.HasAny(id, recorder)
}
