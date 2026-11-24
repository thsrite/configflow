package monitor

// The parts of backend/agents/oxidns_ranking_history.py the overview reads
// (constants, supported, get_history(...).query / .has_any).

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

// HistoryAnswer is a stored ranking query result. First and Last are
// meaningful only when Slots > 0.
type HistoryAnswer struct {
	Records, BlockedRecords, BlockedSlots, Slots, PartialSlots int64
	First, Last                                                int64
	Rows                                                       map[string][]HistoryRow // clients, domains, blocked
}
