package check

import (
	"bytes"
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
	if port <= 0 || port == 443 {
		return "https://" + host + "/"
	}
	return "https://" + host + ":" + strconv.Itoa(port) + "/"
}

// PathStats holds active probe statistics for one path (direct or proxy).
type PathStats struct {
	OK     model.Int            `json:"ok"`
	Total  model.Int            `json:"total"`
	Delays []float64            `json:"delays"`
	Errors model.Map[model.Int] `json:"errors"`
	Fields model.Fields
	raw    model.Raw
}

// UnmarshalJSON decodes PathStats under model R5 rules.
func (s *PathStats) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, s, &s.raw)
}

// MarshalJSON encodes PathStats preserving stored member order and raw bytes.
func (s PathStats) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&s, &s.raw)
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
	Fields     model.Fields
	raw        model.Raw
}

// UnmarshalJSON decodes Suggestion under model R5 rules.
func (s *Suggestion) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, s, &s.raw)
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
	Probe  json.RawMessage `json:"probe"`
	Fields model.Fields
	raw    model.Raw
}

// UnmarshalJSON decodes HostProbe under model R5 rules.
func (h *HostProbe) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, h, &h.raw)
}

// MarshalJSON encodes HostProbe preserving stored member order.
func (h HostProbe) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&h, &h.raw)
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

// TimedRecord is a stored map entry timestamped by checked_at, retaining all
// additional JSON fields verbatim.
type TimedRecord struct {
	CheckedAt StrictString `json:"checked_at"`
	Fields    model.Fields
	raw       model.Raw
}

// UnmarshalJSON decodes TimedRecord while preserving unknown fields and order.
func (r *TimedRecord) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, r, &r.raw)
}

// MarshalJSON encodes TimedRecord preserving stored member order and raw bytes.
func (r TimedRecord) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&r, &r.raw)
}

type probeFile struct {
	Hosts  model.Map[TimedRecord] `json:"hosts"`
	Fields model.Fields
	raw    model.Raw
}

func (f *probeFile) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, f, &f.raw)
}

func (f probeFile) MarshalJSON() ([]byte, error) {
	return model.EncodeRecord(&f, &f.raw)
}

func isNullTimedMap(m model.Map[TimedRecord]) bool {
	encoded, err := m.MarshalJSON()
	return err == nil && bytes.Equal(bytes.TrimSpace(encoded), []byte("null"))
}

func newEmptyProbeFile() probeFile {
	var file probeFile
	_ = model.SetMemberJSON(&file, &file.raw, "hosts", json.RawMessage("{}"))
	return file
}

var probeLock sync.Mutex

// ProbeStore persists per-Agent active domain probe results on disk.
type ProbeStore struct{ files *jsonstore.Store }

// NewProbeStore creates a ProbeStore rooted at dir.
func NewProbeStore(dir string) *ProbeStore { return &ProbeStore{files: jsonstore.New(dir)} }

// Path returns the JSON file path for agentID.
func (s *ProbeStore) Path(agentID string) string { return s.files.Path(agentID) }

func (s *ProbeStore) readFile(agentID string) probeFile {
	var rawObj rawJSONObject
	if !s.files.Read(agentID, &rawObj) || !rawObj.Fields.Has("hosts") {
		return newEmptyProbeFile()
	}
	encoded, err := rawObj.MarshalJSON()
	if err != nil {
		return newEmptyProbeFile()
	}
	var file probeFile
	if err := json.Unmarshal(encoded, &file); err != nil || model.IssueUnder(file.raw.Issues(), "hosts") || isNullTimedMap(file.Hosts) {
		return newEmptyProbeFile()
	}
	return file
}

func pruneHosts(hosts *model.Map[TimedRecord], now time.Time) {
	bound := oldest(now, ProbeRetentionDays)
	var expired []string
	for host, entry := range hosts.All() {
		if string(entry.CheckedAt) < bound {
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
			return string(left.CheckedAt) > string(right.CheckedAt)
		})
		for _, host := range ranked[MaxProbedHosts:] {
			hosts.Delete(host)
		}
	}
}

// SaveEntries merges results into agentID's probe file, prunes expired/excess
// entries relative to now (zero uses time.Now), and writes atomically.
func (s *ProbeStore) SaveEntries(agentID string, results model.Map[TimedRecord], now time.Time) error {
	if now.IsZero() {
		now = time.Now()
	}
	if isNullTimedMap(results) || len(model.Issues(results)) > 0 {
		return valueError(msgInvalidProbeEntry)
	}
	probeLock.Lock()
	defer probeLock.Unlock()
	file := s.readFile(agentID)
	for host, entry := range results.All() {
		file.Hosts.Set(host, entry)
	}
	pruneHosts(&file.Hosts, now)
	_, err := s.files.Write(agentID, &file, false)
	return err
}

// LoadEntries loads and prunes agentID's host probe entries relative to now
// without writing back to disk.
func (s *ProbeStore) LoadEntries(agentID string, now time.Time) model.Map[TimedRecord] {
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
	probeLock.Lock()
	defer probeLock.Unlock()
	return s.files.Remove(agentID)
}

// LatestProbeEntries returns the newest probe entry per host across agentIDs,
// annotated with agent_id.
func LatestProbeEntries(agentIDs []string, store *ProbeStore) (model.Map[TimedRecord], error) {
	var merged model.Map[TimedRecord]
	for _, agentID := range agentIDs {
		agentJSON, err := json.Marshal(agentID)
		if err != nil {
			return merged, fmt.Errorf("encode agent_id: %w", err)
		}
		hosts := store.LoadEntries(agentID, time.Time{})
		for host, entry := range hosts.All() {
			if previous, seen := merged.Get(host); seen && !(string(entry.CheckedAt) > string(previous.CheckedAt)) {
				continue
			}
			copied := entry
			if err := copied.Fields.Set("agent_id", agentJSON); err != nil {
				return merged, fmt.Errorf("set probe agent_id: %w", err)
			}
			merged.Set(host, copied)
		}
	}
	return merged, nil
}
