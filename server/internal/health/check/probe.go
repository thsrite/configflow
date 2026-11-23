package check

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"configflow/internal/jsonstore"
	"configflow/internal/model"
)

const (
	// ProbeRetentionDays is the retention window in days for stored host probes.
	ProbeRetentionDays = 30
	// MaxProbedHosts is the maximum number of hosts retained per Agent.
	MaxProbedHosts = 5000
	// MaxConfidence is the upper bound on domain probe verdict confidence.
	MaxConfidence = 99
)

// ProbeTarget identifies a control or domain probe target.
type ProbeTarget struct {
	Host string `json:"host"`
	URL  string `json:"url"`
}

// ControlTargetsData holds the direct and proxy control probe targets.
type ControlTargetsData struct {
	Direct ProbeTarget `json:"direct"`
	Proxy  ProbeTarget `json:"proxy"`
}

// DefaultControlTargets returns fresh control targets for direct and proxy paths.
func DefaultControlTargets() ControlTargetsData {
	return ControlTargetsData{
		Direct: ProbeTarget{Host: "www.baidu.com", URL: "https://www.baidu.com/"},
		Proxy:  ProbeTarget{Host: "www.gstatic.com", URL: DefaultProbeURL},
	}
}

// VerdictOrder lists domain probe verdicts in display order.
var VerdictOrder = []string{
	"needs_proxy", "direct_only", "both_ok", "unreachable",
	"proxy_down", "direct_down", "flaky", "incomplete",
}

// VerdictLabels maps each domain probe verdict to its Chinese label.
var VerdictLabels = map[string]string{
	"needs_proxy": labelNeedsProxy,
	"direct_only": labelDirectOnly,
	"both_ok":     labelBothOK,
	"unreachable": labelUnreachable,
	"proxy_down":  labelProxyDown,
	"direct_down": labelDirectDown,
	"flaky":       labelFlaky,
	"incomplete":  labelIncomplete,
}

// ProbeURL builds the active probe URL for host and port (0 or 443 uses default HTTPS).
func ProbeURL(host string, port int) string {
	if port == 80 {
		return "http://" + host + "/"
	}
	if port == 0 || port == 443 {
		return "https://" + host + "/"
	}
	return "https://" + host + ":" + strconv.Itoa(port) + "/"
}

// PathStats holds active probe statistics for one path (direct or proxy). A
// value of the wrong type reads as its zero value.
type PathStats struct {
	OK     model.Int                   `json:"ok"`
	Total  model.Int                   `json:"total"`
	Delays []float64                   `json:"delays"`
	Errors model.OrderedMap[model.Int] `json:"errors"`
}

// UnmarshalJSON decodes reported statistics leniently; a delay list with an
// element that is not a number reads as no delays.
func (s *PathStats) UnmarshalJSON(data []byte) error {
	type wire PathStats
	var decoded wire
	issues, err := model.DecodeStored(data, &decoded, "")
	if err != nil {
		return err
	}
	if model.IssueUnder(issues, "delays") {
		decoded.Delays = nil
	}
	*s = PathStats(decoded)
	return nil
}

func pathRate(stats *PathStats) (float64, bool) {
	if stats == nil || stats.Total == 0 {
		return 0, false
	}
	return float64(stats.OK) / float64(stats.Total), true
}

func medianDelay(delays []float64) (float64, bool) {
	if len(delays) == 0 {
		return 0, false
	}
	for _, d := range delays {
		if math.IsNaN(d) || math.IsInf(d, 0) {
			return 0, false
		}
	}
	sorted := slices.Clone(delays)
	slices.Sort(sorted)
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2], true
	}
	mid := (sorted[n/2-1] + sorted[n/2]) / 2
	if math.IsNaN(mid) || math.IsInf(mid, 0) {
		return 0, false
	}
	return mid, true
}

func roundEvenString(value float64) string {
	rounded, _ := new(big.Float).SetFloat64(math.RoundToEven(value)).Int(nil)
	return rounded.String()
}

// PathHealthy reports whether at least half of the probes in stats succeeded.
func PathHealthy(stats *PathStats) bool {
	r, ok := pathRate(stats)
	return ok && r >= 0.5
}

// JudgeOptions configures control-path health and passive traffic signals for JudgePath.
type JudgeOptions struct {
	PassiveFailing bool
	DirectHealthy  bool
	ProxyHealthy   bool
}

// DefaultJudgeOptions returns JudgeOptions with both control paths healthy.
func DefaultJudgeOptions() JudgeOptions {
	return JudgeOptions{DirectHealthy: true, ProxyHealthy: true}
}

// JudgeResult holds the verdict, suggested target, confidence, and reasons from JudgePath.
type JudgeResult struct {
	Verdict    string       `json:"verdict"`
	Target     NullableText `json:"target"`
	Confidence int64        `json:"confidence"`
	Reasons    []string     `json:"reasons"`
}

// JudgePath evaluates direct and proxy probe statistics into a routing verdict.
func JudgePath(direct, proxy *PathStats, options JudgeOptions) JudgeResult {
	reasons := []string{}
	finish := func(verdict, target string, confidence int64) JudgeResult {
		var nullableTarget NullableText
		if target == "" {
			confidence = 0
		} else {
			nullableTarget = NullableText{Value: target, Valid: true}
			if confidence > MaxConfidence {
				confidence = MaxConfidence
			}
		}
		return JudgeResult{
			Verdict:    verdict,
			Target:     nullableTarget,
			Confidence: confidence,
			Reasons:    reasons,
		}
	}
	if proxy != nil {
		if noPath, ok := proxy.Errors.Get("no_path"); ok && noPath != 0 {
			reasons = append(reasons, msgProbeNoPath)
			return finish("incomplete", "", 0)
		}
	}
	dr, okDirect := pathRate(direct)
	pr, okProxy := pathRate(proxy)
	if !okDirect || !okProxy {
		reasons = append(reasons, msgProbeIncomplete)
		return finish("incomplete", "", 0)
	}
	reasons = append(reasons, fmt.Sprintf(msgProbeCounts, int64(direct.OK), int64(direct.Total), int64(proxy.OK), int64(proxy.Total)))

	if dr == 0 && pr >= 0.5 {
		if !options.DirectHealthy {
			reasons = append(reasons, msgProbeDirectDown)
			return finish("direct_down", "", 0)
		}
		confidence := int64(70)
		if pr == 1 && direct.Total >= 2 {
			confidence += 15
			reasons = append(reasons, msgProbeConsistent)
		}
		if options.PassiveFailing {
			confidence += 15
			reasons = append(reasons, msgProbePassiveFailing)
		}
		return finish("needs_proxy", "proxy", confidence)
	}
	if dr >= 0.5 && pr == 0 {
		if !options.ProxyHealthy {
			reasons = append(reasons, msgProbeProxyDown)
			return finish("proxy_down", "", 0)
		}
		confidence := int64(70)
		if dr == 1 && proxy.Total >= 2 {
			confidence += 15
			reasons = append(reasons, msgProbeConsistent)
		}
		return finish("direct_only", "direct", confidence)
	}
	if dr >= 0.5 && pr >= 0.5 {
		directDelay, okD := medianDelay(direct.Delays)
		proxyDelay, okP := medianDelay(proxy.Delays)
		if okD && okP {
			reasons = append(reasons, fmt.Sprintf(msgProbeDelays, roundEvenString(directDelay), roundEvenString(proxyDelay)))
			if directDelay <= proxyDelay*1.5 {
				return finish("both_ok", "direct", 60)
			}
			return finish("both_ok", "proxy", 50)
		}
		return finish("both_ok", "", 0)
	}
	if dr == 0 && pr == 0 {
		if !options.ProxyHealthy {
			reasons = append(reasons, msgProbeProxyDown)
			return finish("proxy_down", "", 0)
		}
		if !options.DirectHealthy {
			reasons = append(reasons, msgProbeDirectDown)
			return finish("direct_down", "", 0)
		}
		return finish("unreachable", "", 0)
	}
	return finish("flaky", "", 0)
}

// Suggestion summarizes the chosen subdomain probe verdict for a domain.
type Suggestion struct {
	Host       NullableText    `json:"host"`
	Verdict    NullableText    `json:"verdict"`
	Target     NullableText    `json:"target"`
	Confidence *model.Int      `json:"confidence"`
	Reasons    json.RawMessage `json:"reasons"`
	CheckedAt  NullableText    `json:"checked_at"`
}

// UnmarshalJSON decodes a probe leniently: a member of the wrong type reads
// as absent.
func (s *Suggestion) UnmarshalJSON(data []byte) error {
	type wire Suggestion
	var decoded wire
	if _, err := model.DecodeStored(data, &decoded, ""); err != nil {
		return err
	}
	*s = Suggestion(decoded)
	return nil
}

// MarshalJSON encodes Suggestion with explicit null fields when unset.
func (s Suggestion) MarshalJSON() ([]byte, error) {
	type wire struct {
		Host       NullableText    `json:"host"`
		Verdict    NullableText    `json:"verdict"`
		Target     NullableText    `json:"target"`
		Confidence *model.Int      `json:"confidence"`
		Reasons    json.RawMessage `json:"reasons"`
		CheckedAt  NullableText    `json:"checked_at"`
	}
	reasons := s.Reasons
	if len(bytes.TrimSpace(reasons)) == 0 {
		reasons = json.RawMessage("null")
	}
	return json.Marshal(wire{
		Host:       s.Host,
		Verdict:    s.Verdict,
		Target:     s.Target,
		Confidence: s.Confidence,
		Reasons:    reasons,
		CheckedAt:  s.CheckedAt,
	})
}

// HostProbe holds an optional active probe record attached to a discovered host.
type HostProbe struct {
	Probe json.RawMessage `json:"probe,omitzero"`
}

// DomainSuggestion picks the highest-confidence actionable probe among hosts,
// falling back to the newest probe, or returns nil when no host was probed.
func DomainSuggestion(hosts []HostProbe) *Suggestion {
	var probed []Suggestion
	for _, host := range hosts {
		trimmed := bytes.TrimSpace(host.Probe)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			continue
		}
		var rawObj rawJSONObject
		if err := json.Unmarshal(trimmed, &rawObj); err != nil || rawObj.Fields.Len() == 0 {
			continue
		}
		var item Suggestion
		if err := json.Unmarshal(trimmed, &item); err != nil {
			continue
		}
		probed = append(probed, item)
	}
	if len(probed) == 0 {
		return nil
	}
	var actionable []Suggestion
	for _, probe := range probed {
		if probe.Target.Valid && probe.Target.Value != "" {
			actionable = append(actionable, probe)
		}
	}
	var best Suggestion
	if len(actionable) > 0 {
		best = actionable[0]
		bestConf := suggestionConfidence(best)
		for _, probe := range actionable[1:] {
			if conf := suggestionConfidence(probe); conf > bestConf {
				best, bestConf = probe, conf
			}
		}
	} else {
		best = probed[0]
		bestAt := best.CheckedAt.Value
		for _, probe := range probed[1:] {
			if probe.CheckedAt.Value > bestAt {
				best, bestAt = probe, probe.CheckedAt.Value
			}
		}
	}
	if len(bytes.TrimSpace(best.Reasons)) == 0 {
		best.Reasons = json.RawMessage("null")
	}
	return &best
}

func suggestionConfidence(s Suggestion) int64 {
	if s.Confidence == nil {
		return 0
	}
	return int64(*s.Confidence)
}

// ---------- store ----------

// TimedRecord is a stored probe or check result: the Agent's result members
// in their order, timestamped by checked_at.
type TimedRecord struct{ model.Fields }

// CheckedAt is the text of checked_at ("" when absent or not text; such an
// entry expires during pruning).
func (r TimedRecord) CheckedAt() string {
	raw, ok := r.Get("checked_at")
	if !ok {
		return ""
	}
	var stamp StrictString
	if json.Unmarshal(raw, &stamp) != nil {
		return ""
	}
	return string(stamp)
}

// probeFile is one Agent's probe file: {"hosts": {...}}.
type probeFile struct {
	Hosts model.OrderedMap[TimedRecord] `json:"hosts"`
}

var probeLock sync.Mutex

// ProbeStore persists per-Agent active domain probe results on disk.
type ProbeStore struct{ files *jsonstore.Store }

// NewProbeStore creates a ProbeStore rooted at dir.
func NewProbeStore(dir string) *ProbeStore { return &ProbeStore{files: jsonstore.New(dir)} }

// Path returns the JSON file path for agentID.
func (s *ProbeStore) Path(agentID string) string { return s.files.Path(agentID) }

// readFile reads an Agent's probe file. A file without a hosts object, or
// with a host entry that is not an object, reads as empty: probe results are
// a cache the next probe refills.
func (s *ProbeStore) readFile(agentID string) probeFile {
	var object model.Fields
	if !s.files.Read(agentID, &object) {
		return probeFile{}
	}
	raw, ok := object.Get("hosts")
	if !ok {
		return probeFile{}
	}
	var file probeFile
	if !decodeClean(raw, &file.Hosts) {
		return probeFile{}
	}
	return file
}

func validTimedRecords(records model.OrderedMap[TimedRecord]) bool {
	for _, entry := range records.All() {
		if !validTimestamp(entry) {
			return false
		}
	}
	return true
}

func validTimestamp(entry TimedRecord) bool {
	value, present := entry.Get("checked_at")
	// Missing timestamps expire during pruning; an explicitly malformed one is invalid.
	if !present {
		return true
	}
	var stamp StrictString
	return json.Unmarshal(value, &stamp) == nil
}

func pruneHosts(hosts *model.OrderedMap[TimedRecord], now time.Time) {
	bound := oldest(now, ProbeRetentionDays)
	var expired []string
	for host, entry := range hosts.All() {
		if entry.CheckedAt() < bound {
			expired = append(expired, host)
		}
	}
	for _, host := range expired {
		hosts.Delete(host)
	}
	if hosts.Len() > MaxProbedHosts {
		ranked := hosts.Keys()
		sort.SliceStable(ranked, func(i, j int) bool {
			left, _ := hosts.Get(ranked[i])
			right, _ := hosts.Get(ranked[j])
			return left.CheckedAt() > right.CheckedAt()
		})
		for _, host := range ranked[MaxProbedHosts:] {
			hosts.Delete(host)
		}
	}
}

// SaveEntries merges results into agentID's probe file, prunes expired/excess
// entries relative to now (zero uses time.Now), and writes atomically.
func (s *ProbeStore) SaveEntries(agentID string, results model.OrderedMap[TimedRecord], now time.Time) error {
	return s.SaveEntriesContext(context.Background(), agentID, results, now)
}

// SaveEntriesContext merges probe entries, retaining the caller's owner lifetime
// through lock acquisition and checking cancellation before file publication.
func (s *ProbeStore) SaveEntriesContext(ctx context.Context, agentID string, results model.OrderedMap[TimedRecord], now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now()
	}
	if !validTimedRecords(results) {
		return valueError(msgInvalidProbeEntry)
	}

	probeLock.Lock()
	defer probeLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	file := s.readFile(agentID)
	file.Hosts.SetAll(results.All())
	pruneHosts(&file.Hosts, now)
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.files.WriteContext(ctx, agentID, &file, false)
	return err
}

// LoadEntries loads and prunes agentID's host probe entries relative to now
// without writing back to disk.
func (s *ProbeStore) LoadEntries(agentID string, now time.Time) model.OrderedMap[TimedRecord] {
	if now.IsZero() {
		now = time.Now()
	}
	probeLock.Lock()
	file := s.readFile(agentID)
	probeLock.Unlock()
	pruneHosts(&file.Hosts, now)
	return file.Hosts
}

// Delete removes the probe store file for agentID.
func (s *ProbeStore) Delete(agentID string) error {
	return s.DeleteContext(context.Background(), agentID)
}

// DeleteContext removes the probe file only while its owner is active.
func (s *ProbeStore) DeleteContext(ctx context.Context, agentID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	probeLock.Lock()
	defer probeLock.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.files.RemoveContext(ctx, agentID)
}

// LatestProbeEntries returns the newest probe entry per host across agentIDs,
// annotated with agent_id.
func LatestProbeEntries(agentIDs []string, store *ProbeStore) (model.OrderedMap[TimedRecord], error) {
	var merged model.OrderedMap[TimedRecord]
	// Collect first and build the Map once: a Set per host copies the whole
	// Map, which made this quadratic in the number of probed hosts.
	var order []string
	latest := map[string]TimedRecord{}
	for _, agentID := range agentIDs {
		agentJSON, err := json.Marshal(agentID)
		if err != nil {
			return merged, fmt.Errorf("encode agent_id: %w", err)
		}
		hosts := store.LoadEntries(agentID, time.Time{})
		for host, entry := range hosts.All() {
			previous, seen := latest[host]
			if seen && !(entry.CheckedAt() > previous.CheckedAt()) {
				continue
			}
			copied := entry
			if err := copied.Fields.Set("agent_id", agentJSON); err != nil {
				return merged, fmt.Errorf("set probe agent_id: %w", err)
			}
			if !seen {
				order = append(order, host)
			}
			latest[host] = copied
		}
	}
	merged.SetAll(func(yield func(string, TimedRecord) bool) {
		for _, host := range order {
			if !yield(host, latest[host]) {
				return
			}
		}
	})
	return merged, nil
}
