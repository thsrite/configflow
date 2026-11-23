package monitor

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// RankingRow is one validated count and its share of the sample.
type RankingRow struct {
	Key   string        `json:"key"`
	Count int64         `json:"count"`
	Share *MetricNumber `json:"share"`
}

// RankingSection is a bounded, stably sorted count sample.
type RankingSection struct {
	SampleSize int64        `json:"sample_size"`
	Rows       []RankingRow `json:"rows"`
	AtLimit    bool         `json:"at_limit"`
}

// BlockedRanking describes the confirmed block path, or why it is unavailable.
type BlockedRanking struct {
	Available    bool
	Code, Reason string
	Section      *RankingSection
	MatcherTag   *string
	MatcherTags  []string
	Unavailable  *Rankings
}

// MarshalJSON writes the available or unavailable block-path variant.
func (b BlockedRanking) MarshalJSON() ([]byte, error) {
	if b.Unavailable != nil {
		return json.Marshal(b.Unavailable)
	}
	return json.Marshal(struct {
		Available bool   `json:"available"`
		Code      string `json:"code"`
		Reason    string `json:"reason"`
		*RankingSection
		MatcherTag  *string   `json:"matcher_tag,omitempty"`
		MatcherTags *[]string `json:"matcher_tags,omitempty"`
	}{b.Available, b.Code, b.Reason, b.Section, b.MatcherTag, optionalTags(b.MatcherTags)})
}
func optionalTags(tags []string) *[]string {
	if tags == nil {
		return nil
	}
	return &tags
}

// Rankings is either a live/historical sample or a typed unavailable section.
// The explicit variants preserve absent/null distinctions at the HTTP boundary.
type Rankings struct {
	Available                                        bool
	Code, Reason                                     string
	Bounds                                           bool
	SinceMS, UntilMS                                 int64
	Recorder                                         string
	CollectedAtMS                                    int64
	InstanceID, SourceRevision, RuntimeRevision      string
	RetentionDays                                    json.RawMessage
	Clients, Domains                                 *RankingSection
	Blocked                                          *BlockedRanking
	Source, Range                                    string
	FirstMS, SlotSeconds, MissingSlots, PartialSlots int64
	History, HistoryVersion                          string
}

// MarshalJSON writes precisely the fields of a live, history or unavailable result.
func (r Rankings) MarshalJSON() ([]byte, error) {
	if !r.Available {
		var since, until *int64
		if r.Bounds {
			since, until = &r.SinceMS, &r.UntilMS
		}
		return json.Marshal(struct {
			Available  bool         `json:"available"`
			Success    bool         `json:"success"`
			Code       string       `json:"code"`
			Reason     string       `json:"reason"`
			SampleSize int64        `json:"sample_size"`
			Rows       []RankingRow `json:"rows"`
			AtLimit    bool         `json:"at_limit"`
			Since      *int64       `json:"since_ms,omitempty"`
			Until      *int64       `json:"until_ms,omitempty"`
		}{false, false, r.Code, r.Reason, 0, []RankingRow{}, false, since, until})
	}
	retention := r.RetentionDays
	if retention == nil {
		retention = []byte("null")
	}
	var first, slot, missing, partial *int64
	if r.Source == "history" {
		first, slot, missing, partial = &r.FirstMS, &r.SlotSeconds, &r.MissingSlots, &r.PartialSlots
	}
	return json.Marshal(struct {
		Available       bool            `json:"available"`
		Success         bool            `json:"success"`
		Recorder        string          `json:"recorder"`
		CollectedAtMS   int64           `json:"collected_at_ms"`
		InstanceID      string          `json:"instance_id"`
		SourceRevision  string          `json:"source_revision"`
		RuntimeRevision string          `json:"runtime_revision"`
		RetentionDays   json.RawMessage `json:"retention_days"`
		SampleLimit     int64           `json:"sample_limit"`
		SinceMS         int64           `json:"since_ms"`
		UntilMS         int64           `json:"until_ms"`
		Clients         *RankingSection `json:"clients"`
		Domains         *RankingSection `json:"domains"`
		Blocked         *BlockedRanking `json:"blocked"`
		Source          string          `json:"source,omitempty"`
		Range           string          `json:"range,omitempty"`
		FirstMS         *int64          `json:"first_ms,omitempty"`
		SlotSeconds     *int64          `json:"slot_seconds,omitempty"`
		MissingSlots    *int64          `json:"missing_slots,omitempty"`
		PartialSlots    *int64          `json:"partial_slots,omitempty"`
		History         string          `json:"history,omitempty"`
		HistoryVersion  string          `json:"history_version,omitempty"`
	}{true, true, r.Recorder, r.CollectedAtMS, r.InstanceID, r.SourceRevision, r.RuntimeRevision, retention, ClientSampleLimit,
		r.SinceMS, r.UntilMS, r.Clients, r.Domains, r.Blocked, r.Source, r.Range, first, slot, missing, partial, r.History, r.HistoryVersion})
}

func unavailableRankings(code string) *Rankings {
	return &Rankings{Code: code, Reason: rankingReason(code)}
}

func decodeRankingSection(raw json.RawMessage, kind string, limit int) (*RankingSection, error) {
	invalid := NewError("INVALID_RESPONSE", 0, "")
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return nil, invalid
	}
	sample, valid := protocolInt(fields, "sample_size", 0, ClientSampleLimit)
	atLimit, present, flagValid := answerBool(fields, "at_limit")
	rawRows, _ := fields.Get("rows")
	var rows []json.RawMessage
	if !valid || !present || !flagValid || atLimit != (sample == ClientSampleLimit) || jsondecode.Unmarshal(rawRows, &rows) != nil || rows == nil || len(rows) > limit {
		return nil, invalid
	}
	factor := int64(65535)
	if kind == "clients" {
		factor = 1
	}
	result := &RankingSection{SampleSize: sample, AtLimit: atLimit, Rows: make([]RankingRow, 0, len(rows))}
	seen := map[string]bool{}
	total := int64(0)
	for _, raw := range rows {
		var row model.Fields
		if jsondecode.Unmarshal(raw, &row) != nil {
			return nil, invalid
		}
		key := fieldText(row, "key")
		count, valid := protocolInt(row, "count", 1, sample*factor)
		if key == "" || runeLen(key) > 1024 || hasControl(key) || seen[key] || !valid || kind == "clients" && !isPlainIP(key) {
			return nil, invalid
		}
		seen[key] = true
		total += count
		result.Rows = append(result.Rows, RankingRow{key, count, metricRatio(metricInt(count), metricInt(sample), false)})
	}
	if total > sample*factor {
		return nil, invalid
	}
	sort.SliceStable(result.Rows, func(i, j int) bool {
		a, b := result.Rows[i], result.Rows[j]
		return a.Count > b.Count || a.Count == b.Count && a.Key < b.Key
	})
	return result, nil
}

func liveRankings(ctx context.Context, m Registry, a *model.Agent, params query, stamp *SnapshotIdentity, state *DashboardState, recorders []string) (*Rankings, error) {
	since := max(state.StartedAtMS, stamp.CollectedAtMS-RankingWindowMS)
	selected, hasSelected := params.get("recorder")
	if hasSelected && selected != "" && !slices.Contains(recorders, selected) {
		return nil, invalidQuery()
	}
	if len(recorders) == 0 {
		r := unavailableRankings("recorder_not_configured")
		r.Bounds = true
		r.SinceMS = since
		r.UntilMS = stamp.CollectedAtMS
		return r, nil
	}
	request := append(query{}, params...)
	request = request.set("since_ms", strconv.FormatInt(since, 10))
	request = request.set("until_ms", strconv.FormatInt(stamp.CollectedAtMS, 10))
	fields, err := requestNative(ctx, m, a, "rankings", request, MinDashboardAgentVersion)
	if err != nil {
		return nil, err
	}
	invalid := NewError("INVALID_RESPONSE", 0, "")
	recorder := fieldText(fields, "recorder")
	collected, valid := protocolInt(fields, "collected_at_ms", 0, 253402300799999)
	sampleLimit, sampleValid := protocolInt(fields, "sample_limit", ClientSampleLimit, ClientSampleLimit)
	_ = sampleLimit
	retention, present := fields.Get("retention_days")
	if present && string(retention) != "null" {
		if _, valid := protocolInt(fields, "retention_days", 0, 36500); !valid {
			return nil, invalid
		}
	}
	if !requiredBool(fields, "success", true) || !validRecorderTag(recorder) || !slices.Contains(recorders, recorder) || hasSelected && recorder != selected || !valid || !sampleValid {
		return nil, invalid
	}
	if fieldText(fields, "instance_id") != state.InstanceID || fieldText(fields, "source_revision") != state.SourceRevision || fieldText(fields, "runtime_revision") != state.RuntimeRevision || collected < stamp.CollectedAtMS {
		return nil, NewError("SNAPSHOT_CHANGED", 409, "")
	}
	raw, _ := fields.Get("blocked")
	var blocked model.Fields
	if jsondecode.Unmarshal(raw, &blocked) != nil {
		return nil, invalid
	}
	available, present, valid := answerBool(blocked, "available")
	if !present || !valid {
		return nil, invalid
	}
	b := &BlockedRanking{Available: available}
	if available {
		code := fieldText(blocked, "reason")
		if !blocked.Has("reason") {
			code = "standard_adguard"
		}
		if !reasonRE.MatchString(code) {
			return nil, invalid
		}
		label, known := blockedReasons[code]
		if !known {
			label = msgBlockedPath
		}
		b.Code, b.Reason = code, label
		b.Section, err = decodeRankingSection(raw, "domains", 10)
		if err != nil {
			return nil, err
		}
		if blocked.Has("matcher_tag") {
			tag := fieldText(blocked, "matcher_tag")
			if !validRecorderTag(tag) {
				return nil, invalid
			}
			b.MatcherTag = &tag
		}
		if raw, present := blocked.Get("matcher_tags"); present {
			if jsondecode.Unmarshal(raw, &b.MatcherTags) != nil || len(b.MatcherTags) == 0 || len(b.MatcherTags) > BlockRankingMatcherLimit {
				return nil, invalid
			}
			for _, tag := range b.MatcherTags {
				if !validRecorderTag(tag) {
					return nil, invalid
				}
			}
		}
	} else {
		code := fieldText(blocked, "reason")
		if !reasonRE.MatchString(code) {
			code = "unsupported_configuration"
		}
		b.Unavailable = unavailableRankings(code)
	}
	raw, _ = fields.Get("clients")
	clients, err := decodeRankingSection(raw, "clients", 10)
	if err != nil {
		return nil, err
	}
	raw, _ = fields.Get("domains")
	domains, err := decodeRankingSection(raw, "domains", 10)
	if err != nil {
		return nil, err
	}
	return &Rankings{Available: true, Recorder: recorder, CollectedAtMS: collected, InstanceID: state.InstanceID, SourceRevision: state.SourceRevision, RuntimeRevision: state.RuntimeRevision,
		RetentionDays: retention, SinceMS: since, UntilMS: stamp.CollectedAtMS, Clients: clients, Domains: domains, Blocked: b}, nil
}

type rankingCacheIdentity struct{ directory, id string }
type rankingCacheKey struct{ recorder, recorders, generation, instance, source, runtime string }
type cachedRanking struct {
	key      rankingCacheKey
	result   *Rankings
	at       float64
	failedAt *float64
}

var nativeRankingsMu sync.Mutex
var nativeRankingsCache = map[rankingCacheIdentity]*cachedRanking{}

func cloneRankings(r *Rankings) *Rankings {
	if r == nil {
		return nil
	}
	out := *r
	out.RetentionDays = append(json.RawMessage(nil), r.RetentionDays...)
	cloneSection := func(s *RankingSection) *RankingSection {
		if s == nil {
			return nil
		}
		out := *s
		out.Rows = append([]RankingRow{}, s.Rows...)
		for i := range out.Rows {
			out.Rows[i].Share = cloneMetric(s.Rows[i].Share)
		}
		return &out
	}
	out.Clients, out.Domains = cloneSection(r.Clients), cloneSection(r.Domains)
	if r.Blocked != nil {
		b := *r.Blocked
		b.Section = cloneSection(r.Blocked.Section)
		b.MatcherTags = append([]string(nil), r.Blocked.MatcherTags...)
		if r.Blocked.MatcherTag != nil {
			tag := *r.Blocked.MatcherTag
			b.MatcherTag = &tag
		}
		b.Unavailable = cloneRankings(r.Blocked.Unavailable)
		out.Blocked = &b
	}
	return &out
}
func cachedRankings(ctx context.Context, m Registry, a *model.Agent, params query, stamp *SnapshotIdentity, state *DashboardState, recorders []string, manual bool) (*Rankings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := rankingCacheIdentity{m.DataDir(), a.ID}
	selected, _ := params.get("recorder")
	encoded, _ := json.Marshal(recorders)
	key := rankingCacheKey{selected, string(encoded), state.Generation, state.InstanceID, state.SourceRevision, state.RuntimeRevision}
	now := monotonic()
	nativeRankingsMu.Lock()
	cached := nativeRankingsCache[id]
	if cached != nil && cached.key != key {
		cached = nil
	}
	nativeRankingsMu.Unlock()
	if cached != nil && !manual {
		if cached.result != nil && cached.failedAt == nil && 0 <= now-cached.at && now-cached.at < RankingReuseSeconds {
			return cloneRankings(cached.result), nil
		}
		if cached.failedAt != nil && 0 <= now-*cached.failedAt && now-*cached.failedAt < RankingBackoffSeconds {
			if cached.result != nil {
				return cloneRankings(cached.result), nil
			}
			return unavailableRankings("rankings_paused"), nil
		}
	}
	result, err := liveRankings(ctx, m, a, params, stamp, state, recorders)
	if err != nil {
		var e *Error
		if errors.As(err, &e) && (e.Code == "AGENT_UNREACHABLE" || e.Code == "CORE_UNREACHABLE") {
			var previous *Rankings
			at := now
			if cached != nil {
				previous, at = cached.result, cached.at
			}
			failed := now
			if ctx.Err() == nil {
				nativeRankingsMu.Lock()
				nativeRankingsCache[id] = &cachedRanking{key, previous, at, &failed}
				nativeRankingsMu.Unlock()
			}
			if previous != nil && !manual {
				return cloneRankings(previous), nil
			}
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nativeRankingsMu.Lock()
	nativeRankingsCache[id] = &cachedRanking{key, cloneRankings(result), now, nil}
	nativeRankingsMu.Unlock()
	return result, nil
}

func storedRankings(ctx context.Context, m Registry, a *model.Agent, recorder, rangeKey string, stamp *SnapshotIdentity, state *DashboardState, recorders []string) (*Rankings, error) {
	if len(recorders) == 0 || !slices.Contains(recorders, recorder) || !modelVersionAtLeast(a, HistoryAgentVersion) {
		return nil, nil
	}
	now := floorDiv(stamp.CollectedAtMS, 1000)
	since := floorDiv(state.StartedAtMS, 1000)
	if rangeKey != "round" {
		since = now - HistoryRanges[rangeKey]
	}
	since -= floorMod(since, HistoryHour)
	until := now - floorMod(now, HistoryHour) + HistoryHour
	stored, err := queryHistory(ctx, getHistory(m.DataDir()), a.ID, recorder, since, until)
	if err != nil {
		return nil, err
	}
	if stored.Slots == 0 {
		return nil, nil
	}
	expected := max(int64(1), floorDiv(stored.Last-stored.First, HistorySlot))
	section := func(kind string, total int64) *RankingSection {
		rows := make([]RankingRow, 0, len(stored.Rows[kind]))
		for _, row := range stored.Rows[kind] {
			share := metricInt(0)
			if total != 0 {
				share = metricRatio(metricInt(row.Count), metricInt(total), false)
			}
			rows = append(rows, RankingRow{row.Key, row.Count, share})
		}
		return &RankingSection{SampleSize: total, Rows: rows}
	}
	blocked := &BlockedRanking{Unavailable: unavailableRankings("unsupported_configuration")}
	if stored.BlockedSlots != 0 {
		blocked = &BlockedRanking{Available: true, Code: "history", Reason: msgBlockedPath, Section: section("blocked", stored.BlockedRecords)}
	}
	return &Rankings{Available: true, Source: "history", Range: rangeKey, Recorder: recorder, CollectedAtMS: stored.Last * 1000, InstanceID: state.InstanceID, SourceRevision: state.SourceRevision, RuntimeRevision: state.RuntimeRevision,
		RetentionDays: json.RawMessage(strconv.Itoa(HistoryRetentionDay)), SinceMS: since * 1000, UntilMS: stored.Last * 1000, FirstMS: stored.First * 1000, SlotSeconds: HistorySlot, MissingSlots: max(int64(0), expected-stored.Slots), PartialSlots: stored.PartialSlots,
		Clients: section("clients", stored.Records), Domains: section("domains", stored.Records), Blocked: blocked}, nil
}
func overviewRanking(ctx context.Context, m Registry, a *model.Agent, params query, stamp *SnapshotIdentity, state *DashboardState, recorders []string, manual bool) (*Rankings, error) {
	result, err := func() (*Rankings, error) {
		var effective query
		selected, provided := params.get("recorder")
		if provided {
			effective = query{{"recorder", selected}}
		}
		if !manual && (!provided || !slices.Contains(recorders, selected)) {
			effective = nil
			selected = ""
		}
		if selected == "" && len(recorders) > 0 {
			selected = recorders[0]
		}
		rangeKey, provided := params.get("range")
		if !provided {
			rangeKey = "round"
		}
		result, err := storedRankings(ctx, m, a, selected, rangeKey, stamp, state, recorders)
		if err != nil || result != nil {
			return result, err
		}
		result, err = cachedRankings(ctx, m, a, effective, stamp, state, recorders, manual)
		if err != nil {
			return nil, err
		}
		if result.Available {
			result.HistoryVersion = HistoryAgentVersion
			if !modelVersionAtLeast(a, HistoryAgentVersion) {
				result.History = "upgrade_required"
			} else {
				stored := false
				if slices.Contains(recorders, selected) {
					stored, err = hasHistory(ctx, getHistory(m.DataDir()), a.ID, selected)
					if err != nil {
						return nil, err
					}
				}
				result.History = "collecting"
				if stored {
					result.History = "pending_range"
				}
			}
		}
		return result, nil
	}()
	if err != nil {
		var e *Error
		if !errors.As(err, &e) || manual || e.Code == "INVALID_QUERY" {
			return nil, err
		}
		return unavailableRankings(strings.ToLower(e.Code)), nil
	}
	return result, nil
}
