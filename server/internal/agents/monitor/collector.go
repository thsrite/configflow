package monitor

import (
	"context"
	"errors"
	"strconv"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/redact"
)

func readSlot(ctx context.Context, m Registry, a *model.Agent, recorder string, since, until int64) (*Slot, error) {
	fields, err := requestNative(ctx, m, a, "rankings", query{{"recorder", recorder}, {"since_ms", strconv.FormatInt(since*1000, 10)}, {"until_ms", strconv.FormatInt(until*1000, 10)}, {"rows", strconv.Itoa(RankingRows)}}, HistoryAgentVersion)
	if err != nil {
		return nil, err
	}
	if !requiredBool(fields, "success", true) || fieldText(fields, "recorder") != recorder {
		return nil, ErrSlotIncomplete
	}
	sections := map[string]*RankingSection{}
	for _, kind := range []string{"clients", "domains"} {
		raw, _ := fields.Get(kind)
		section, err := decodeRankingSection(raw, kind, RankingRows)
		if err != nil {
			return nil, err
		}
		sections[kind] = section
	}
	raw, _ := fields.Get("blocked")
	var blocked model.Fields
	if jsondecode.Unmarshal(raw, &blocked) != nil {
		return nil, ErrSlotIncomplete
	}
	available, present, valid := answerBool(blocked, "available")
	if !present || !valid {
		return nil, ErrSlotIncomplete
	}
	if available {
		section, err := decodeRankingSection(raw, "domains", RankingRows)
		if err != nil {
			return nil, err
		}
		sections["blocked"] = section
	}
	atLimit := false
	for _, section := range sections {
		atLimit = atLimit || section.AtLimit
	}
	if atLimit && until-since > RankingMinSlot {
		middle := since + floorDiv(until-since, 2)
		middle -= floorMod(middle, RankingMinSlot)
		first, err := readSlot(ctx, m, a, recorder, since, middle)
		if err != nil {
			return nil, err
		}
		second, err := readSlot(ctx, m, a, recorder, middle, until)
		if err != nil {
			return nil, err
		}
		return mergeSlots(first, second), nil
	}
	slot := &Slot{Records: sections["clients"].SampleSize, Partial: atLimit, Rows: map[string]*Counts{}}
	if blocked, known := sections["blocked"]; known {
		count := blocked.SampleSize
		slot.Blocked = &count
	}
	for _, kind := range RankingKinds {
		section, known := sections[kind]
		if !known {
			continue
		}
		counts := &Counts{Values: map[string]int64{}}
		for _, row := range section.Rows {
			counts.Add(row.Key, row.Count)
		}
		slot.Rows[kind] = counts
	}
	return slot, nil
}

// CollectAgentHistory fills missing slots under the originating runtime context.
func CollectAgentHistory(ctx context.Context, m Registry, store *RankingStore, agent *model.Agent, recorders []string,
	now *int64, budget, retentionDays int, pause time.Duration) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	end := LastCompleteSlotEnd(nowOr(now))
	if retentionDays == 0 {
		retentionDays = RankingCoreRetentionDays
	}
	days := int64(min(HistoryRetentionDay, max(1, retentionDays)))
	var starts []int64
	for start := end - HistorySlot; start > end-days*HistoryDay-1; start -= HistorySlot {
		starts = append(starts, start)
	}
	stored := 0
	agentID, valid := agent.ID, true
	if !valid || agentID == "" {
		return 0, ErrSlotIncomplete
	}
	store.setCaughtUp(agentID, 0, false)
	for _, recorder := range recorders {
		done, err := store.Fetched(ctx, agentID, recorder, starts)
		if err != nil {
			return stored, err
		}
		for _, start := range starts {
			if rankingStopping(ctx) {
				return stored, ctx.Err()
			}
			if done[start] {
				continue
			}
			if budget <= 0 {
				return stored, nil
			}
			budget--
			slot, err := readSlot(ctx, m, agent, recorder, start, start+HistorySlot)
			if err != nil {
				var monitorErr *Error
				if !errors.As(err, &monitorErr) && !errors.Is(err, ErrSlotIncomplete) {
					return stored, err
				}
				name := agentID
				if agent.Name != "" {
					name = agent.Name
				}
				reason := err.Error()
				if monitorErr != nil {
					reason = monitorErr.Code
				}
				// The device is busy or away: try again next round.
				name = redact.DiagnosticText(name, redact.Secrets{agent.Token}, 128)
				rankingLogger.Infof(msgRankingUnavailable, name, reason)
				return stored, nil
			}
			added, err := store.StoreSlot(ctx, agentID, recorder, start, slot)
			if err != nil {
				return stored, err
			}
			if added {
				stored++
			}
			if pause > 0 {
				if !rankingSleep(ctx, pause) {
					return stored, ctx.Err()
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return stored, err
	}
	store.setCaughtUp(agentID, end, true)
	return stored, nil
}

// RoundRegistry adds an owned typed registry listing for background collection.
type RoundRegistry interface {
	Registry
	ReadAgents(context.Context, bool, func(*model.Agent)) ([]model.Agent, error)
}

func historyUsable(m RoundRegistry, a *model.Agent) bool {
	if codec, ok := m.(interface{ historyUsable(*model.Agent) bool }); ok {
		return codec.historyUsable(a)
	}
	return agents.AgentEnabledModel(a) && a.Status == "online" && agents.FreshHeartbeat(a.LastHeartbeat, nil)
}
func historyRecorders(ctx context.Context, m RoundRegistry, a *model.Agent) ([]string, error) {
	if codec, ok := m.(interface {
		historyRecorders(context.Context, *model.Agent) ([]string, error)
	}); ok {
		return codec.historyRecorders(ctx, a)
	}
	snapshot, err := readOverviewSnapshot(ctx, m, a, true)
	if err != nil {
		return nil, err
	}
	return snapshot.Recorders, nil
}
func historyCollect(ctx context.Context, m RoundRegistry, store *RankingStore, a *model.Agent, recorders []string, now *int64) error {
	if codec, ok := m.(interface {
		historyCollect(context.Context, *RankingStore, *model.Agent, []string, *int64) error
	}); ok {
		return codec.historyCollect(ctx, store, a, recorders, now)
	}
	_, err := CollectAgentHistory(ctx, m, store, a, recorders, now, RankingSlotsPerRound, RankingCoreRetentionDays, 200*time.Millisecond)
	return err
}

// CollectHistoryRound visits usable OxiDNS Agents, then expires obsolete rows.
func CollectHistoryRound(ctx context.Context, m RoundRegistry, store *RankingStore, now *int64) error {
	if store == nil {
		store = GetRankingStore(m.DataDir())
	}
	list, err := m.ReadAgents(ctx, false, nil)
	if err != nil {
		return err
	}
	end := LastCompleteSlotEnd(nowOr(now))
	for i := range list {
		agent := &list[i]
		if rankingStopping(ctx) {
			return ctx.Err()
		}
		if agent.ServiceType != "oxidns" {
			continue
		}
		supported := modelVersionAtLeast(agent, HistoryAgentVersion)
		if !supported || !historyUsable(m, agent) {
			continue
		}
		if caught, ok := store.CaughtUp(agent.ID); ok && caught == end {
			continue // nothing new until the next slot ends: no request at all
		}
		recorders, err := historyRecorders(ctx, m, agent)
		if err != nil {
			var monitorErr *Error
			if errors.As(err, &monitorErr) {
				continue
			}
			return err
		}
		if err := historyCollect(ctx, m, store, agent, recorders, now); err != nil {
			return err
		}
	}
	ids := make([]string, len(list))
	for i := range list {
		ids[i] = list[i].ID
	}
	return store.Maintain(ctx, ids, now)
}
