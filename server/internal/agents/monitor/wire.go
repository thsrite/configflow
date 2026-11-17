package monitor

import (
	"configflow/internal/agents"
	"configflow/internal/lifecycle"
)

// Wire is A07's entry in internal/server wirePackages: the ranking history
// store read by the overview (get_history), the publication checkpoint /
// completion hooks of the registry, and the ranking collection loop
// (oxidns_ranking_history.start_loop, which app.py starts in the service
// process; once per process).
func Wire() {
	SetRankingHistory(rankingStoreFactory)
	agents.RegisterHooks(agents.Hooks{PublicationComplete: completeHook, PublicationCheckpoint: checkpointHook})
	lifecycle.RegisterPeriodic(rankingPeriodic())
}
