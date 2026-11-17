package check

import (
	"sort"
	"sync"
	"time"

	"configflow/internal/jsonstore"
	"configflow/internal/jsonx"
)

// Domain probe (backend/utils/domain_probe.py): verdicts, confidence and the
// per-Agent store of active probe results. The probing itself is the
// Agent's (A01 AgentManager.probe_domains, driven by C37).

const (
	ProbeRetentionDays = 30
	MaxProbedHosts     = 5000
	MaxConfidence      = 99
)

// ControlTargets is CONTROL_TARGETS (fresh copies).
func ControlTargets() *jsonx.Object {
	return jsonx.Obj(
		"direct", jsonx.Obj("host", "www.baidu.com", "url", "https://www.baidu.com/"),
		"proxy", jsonx.Obj("host", "www.gstatic.com", "url", "https://www.gstatic.com/generate_204"))
}

// VerdictLabels is VERDICT_LABELS (insertion order kept in VerdictOrder).
var (
	VerdictOrder  = []string{"needs_proxy", "direct_only", "both_ok", "unreachable", "proxy_down", "direct_down", "flaky", "incomplete"}
	VerdictLabels = map[string]string{
		"needs_proxy": "直连不通，代理可达",
		"direct_only": "代理不通，直连可达",
		"both_ok":     "直连与代理都可达",
		"unreachable": "直连与代理都不通",
		"proxy_down":  "代理节点不可用，无法判断",
		"direct_down": "本机直连网络异常，无法判断",
		"flaky":       "结果不稳定",
		"incomplete":  "探测不完整",
	}
)

// ProbeURL is probe_url(host, port): the URL for the port seen most often
// in the traffic. port nil is None.
func ProbeURL(host string, port any) string {
	if jsonx.Equal(port, jsonx.Int(80)) {
		return "http://" + host + "/"
	}
	if !jsonx.Truthy(port) || jsonx.Equal(port, jsonx.Int(443)) {
		return "https://" + host + "/"
	}
	return "https://" + host + ":" + jsonx.Str(port) + "/"
}

// rate is _rate: ok / total, nil when nothing was probed.
func rate(stats any) *pnum {
	if !jsonx.Truthy(stats) || !jsonx.Truthy(get(stats, "total", nil)) {
		return nil
	}
	ok := get(stats, "ok", jsonx.Int(0))
	total := item(stats, "total")
	a, okA := asNumberOK(ok)
	b, okB := asNumberOK(total)
	if !okA || !okB {
		raise("TypeError", "unsupported operand type(s) for /: '"+TypeName(ok)+"' and '"+TypeName(total)+"'")
	}
	value := trueDiv(a, b)
	return &value
}

func asNumberOK(v any) (pnum, bool) {
	switch v.(type) {
	case bool, jsonx.Number:
		return asNumber(v, "", nil), true
	}
	return pnum{}, false
}

// median is statistics.median(stats.get('delays') or []) or nil.
func median(stats any) *pnum {
	delays := get(stats, "delays", nil)
	if !jsonx.Truthy(delays) {
		return nil
	}
	items := iterate(delays)
	values := make([]pnum, len(items))
	for i, element := range items {
		value, ok := asNumberOK(element)
		if !ok {
			raise("TypeError", "'<' not supported between instances of '"+TypeName(element)+"' and 'int'")
		}
		values[i] = value
	}
	sort.SliceStable(values, func(i, j int) bool { return less(values[i], values[j]) })
	n := len(values)
	if n%2 == 1 {
		return &values[n/2]
	}
	value := trueDiv(add(values[n/2-1], values[n/2]), intNum(2))
	return &value
}

// PathHealthy is path_healthy(stats).
func PathHealthy(stats any) (healthy bool, err error) {
	defer catch(&err)
	r := rate(stats)
	return r != nil && greaterEq(*r, floatNum(0.5)), nil
}

// JudgeOptions are judge's keyword arguments (Python defaults: false, true,
// true).
type JudgeOptions struct {
	PassiveFailing bool
	DirectHealthy  bool
	ProxyHealthy   bool
}

// DefaultJudgeOptions are judge's defaults.
func DefaultJudgeOptions() JudgeOptions { return JudgeOptions{DirectHealthy: true, ProxyHealthy: true} }

// Judge is judge(direct, proxy, ...): verdict, suggested target and
// confidence from the probe statistics of both paths.
func Judge(direct, proxy any, options JudgeOptions) (out *jsonx.Object, err error) {
	defer catch(&err)
	directRate, proxyRate := rate(direct), rate(proxy)
	reasons := []string{}
	finish := func(verdict string, target any, confidence int64) *jsonx.Object {
		if !jsonx.Truthy(target) {
			confidence = 0
		} else if confidence > MaxConfidence {
			confidence = MaxConfidence
		}
		return jsonx.Obj("verdict", verdict, "target", target, "confidence", jsonx.Int(confidence),
			"reasons", jsonx.ListOf(reasons))
	}
	if jsonx.Truthy(proxy) && jsonx.Truthy(get(get(proxy, "errors", jsonx.NewObject()), "no_path", nil)) {
		reasons = append(reasons, "代理策略不在当前 Mihomo 配置中，部署配置后再探测")
		return finish("incomplete", nil, 0), nil
	}
	if directRate == nil || proxyRate == nil {
		reasons = append(reasons, "有一条路径没有完成探测")
		return finish("incomplete", nil, 0), nil
	}
	reasons = append(reasons, "直连 "+jsonx.Str(item(direct, "ok"))+"/"+jsonx.Str(item(direct, "total"))+
		"，代理 "+jsonx.Str(item(proxy, "ok"))+"/"+jsonx.Str(item(proxy, "total")))
	dr, pr := *directRate, *proxyRate
	half, one := floatNum(0.5), intNum(1)
	totalAtLeast2 := func(stats any) bool {
		total := item(stats, "total")
		value, ok := asNumberOK(total)
		if !ok {
			raise("TypeError", "'>=' not supported between instances of '"+TypeName(total)+"' and 'int'")
		}
		return greaterEq(value, intNum(2))
	}

	if isZeroNum(dr) && greaterEq(pr, half) {
		if !options.DirectHealthy {
			reasons = append(reasons, "直连对照目标也不通")
			return finish("direct_down", nil, 0), nil
		}
		confidence := int64(70)
		if equalNum(pr, one) && totalAtLeast2(direct) {
			confidence += 15
			reasons = append(reasons, "多次探测结果一致")
		}
		if options.PassiveFailing {
			confidence += 15
			reasons = append(reasons, "真实流量中直连也大量失败")
		}
		return finish("needs_proxy", "proxy", confidence), nil
	}
	if greaterEq(dr, half) && isZeroNum(pr) {
		if !options.ProxyHealthy {
			reasons = append(reasons, "代理对照目标也不通")
			return finish("proxy_down", nil, 0), nil
		}
		confidence := int64(70)
		if equalNum(dr, one) && totalAtLeast2(proxy) {
			confidence += 15
			reasons = append(reasons, "多次探测结果一致")
		}
		return finish("direct_only", "direct", confidence), nil
	}
	if greaterEq(dr, half) && greaterEq(pr, half) {
		directDelay, proxyDelay := median(direct), median(proxy)
		if directDelay != nil && proxyDelay != nil {
			reasons = append(reasons, "延迟：直连 "+round(*directDelay).String()+" ms，代理 "+round(*proxyDelay).String()+" ms")
			if lessEqual(*directDelay, mul(*proxyDelay, floatNum(1.5))) {
				return finish("both_ok", "direct", 60), nil
			}
			return finish("both_ok", "proxy", 50), nil
		}
		return finish("both_ok", nil, 0), nil
	}
	if isZeroNum(dr) && isZeroNum(pr) {
		if !options.ProxyHealthy {
			reasons = append(reasons, "代理对照目标也不通")
			return finish("proxy_down", nil, 0), nil
		}
		if !options.DirectHealthy {
			reasons = append(reasons, "直连对照目标也不通")
			return finish("direct_down", nil, 0), nil
		}
		return finish("unreachable", nil, 0), nil
	}
	return finish("flaky", nil, 0), nil
}

// ---------- store ----------

var probeLock sync.Mutex

// ProbeStore keeps, per Agent, the latest probe result of each host.
type ProbeStore struct{ files *jsonstore.Store }

// NewProbeStore is ProbeStore(data_dir).
func NewProbeStore(dir string) *ProbeStore { return &ProbeStore{jsonstore.New(dir)} }

// Path is the file of an Agent.
func (s *ProbeStore) Path(agentID string) string { return s.files.Path(agentID) }

func (s *ProbeStore) read(agentID string) *jsonx.Object {
	var data *jsonx.Object
	if !s.files.Read(agentID, &data) {
		return jsonx.Obj("hosts", jsonx.NewObject())
	}
	if _, ok := data.Value("hosts").(*jsonx.Object); !ok {
		return jsonx.Obj("hosts", jsonx.NewObject())
	}
	return data
}

func checkedAtOf(entry any) string {
	value := get(entry, "checked_at", "")
	text, ok := value.(string)
	if !ok {
		raise("TypeError", "'<' not supported between instances of '"+TypeName(value)+"' and 'str'")
	}
	return text
}

func prune(data *jsonx.Object, now time.Time) {
	bound := oldest(now, ProbeRetentionDays)
	hosts := data.Value("hosts").(*jsonx.Object)
	var expired []string
	for host, entry := range hosts.All() {
		if checkedAtOf(entry) < bound {
			expired = append(expired, host)
		}
	}
	for _, host := range expired {
		hosts.Delete(host)
	}
	if hosts.Len() > MaxProbedHosts {
		ranked := hosts.Keys()
		sort.SliceStable(ranked, func(i, j int) bool {
			return checkedAtOf(hosts.Value(ranked[i])) > checkedAtOf(hosts.Value(ranked[j]))
		})
		for _, host := range ranked[MaxProbedHosts:] {
			hosts.Delete(host)
		}
	}
}

// Save is save(agent_id, results, now): merge (deep copies), prune, write.
// A zero now is datetime.now().
func (s *ProbeStore) Save(agentID string, results *jsonx.Object, now time.Time) (err error) {
	if now.IsZero() {
		now = time.Now()
	}
	probeLock.Lock()
	defer probeLock.Unlock()
	defer catch(&err)
	data := s.read(agentID)
	hosts := data.Value("hosts").(*jsonx.Object)
	for host, entry := range results.All() {
		hosts.Set(host, jsonx.DeepCopy(entry))
	}
	prune(data, now)
	_, err = s.files.Write(agentID, data, false)
	return err
}

// Load is load(agent_id, now): the hosts, pruned (not written back).
func (s *ProbeStore) Load(agentID string, now time.Time) (hosts *jsonx.Object, err error) {
	if now.IsZero() {
		now = time.Now()
	}
	probeLock.Lock()
	data := s.read(agentID)
	probeLock.Unlock()
	defer catch(&err)
	prune(data, now)
	return data.Value("hosts").(*jsonx.Object), nil
}

// Delete is delete(agent_id).
func (s *ProbeStore) Delete(agentID string) error {
	probeLock.Lock()
	defer probeLock.Unlock()
	return s.files.Remove(agentID)
}

// LatestProbes is latest_probes(agent_ids, store): the newest result per
// host across Agents, with its agent_id.
func LatestProbes(agentIDs []string, store *ProbeStore) (merged *jsonx.Object, err error) {
	merged = jsonx.NewObject()
	for _, agentID := range agentIDs {
		hosts, err := store.Load(agentID, time.Time{})
		if err != nil {
			return nil, err
		}
		if err := mergeHosts(merged, hosts, agentID); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func mergeHosts(merged, hosts *jsonx.Object, agentID string) (err error) {
	defer catch(&err)
	for host, entry := range hosts.All() {
		previous, seen := merged.Get(host)
		if seen && !(greaterValue(get(entry, "checked_at", ""), get(previous, "checked_at", ""))) {
			continue
		}
		object, ok := entry.(*jsonx.Object)
		if !ok {
			raise("TypeError", "'"+TypeName(entry)+"' object is not a mapping")
		}
		copied := object.Clone()
		copied.Set("agent_id", agentID)
		merged.Set(host, copied)
	}
	return nil
}

// greaterValue is a > b for the values Python orders: numbers (and bools)
// with each other, strings with strings.
func greaterValue(a, b any) bool {
	if x, ok := a.(string); ok {
		if y, ok := b.(string); ok {
			return x > y
		}
	}
	x, okA := asNumberOK(a)
	y, okB := asNumberOK(b)
	if !okA || !okB {
		raise("TypeError", "'>' not supported between instances of '"+TypeName(a)+"' and '"+TypeName(b)+"'")
	}
	return less(y, x)
}

// DomainSuggestion is domain_suggestion(hosts): the actionable suggestion
// with the highest confidence among the subdomains, else the newest
// verdict; nil when nothing was probed.
func DomainSuggestion(hosts []any) (out *jsonx.Object, err error) {
	defer catch(&err)
	var probed []any
	for _, host := range hosts {
		if probe := get(host, "probe", nil); jsonx.Truthy(probe) {
			probed = append(probed, probe)
		}
	}
	if len(probed) == 0 {
		return nil, nil
	}
	var actionable []any
	for _, probe := range probed {
		if jsonx.Truthy(get(probe, "target", nil)) {
			actionable = append(actionable, probe)
		}
	}
	var best any
	if len(actionable) > 0 {
		best = actionable[0]
		bestKey := item(best, "confidence")
		for _, probe := range actionable[1:] {
			if key := item(probe, "confidence"); greaterValue(key, bestKey) {
				best, bestKey = probe, key
			}
		}
	} else {
		best = probed[0]
		bestKey := get(best, "checked_at", "")
		for _, probe := range probed[1:] {
			if key := get(probe, "checked_at", ""); greaterValue(key, bestKey) {
				best, bestKey = probe, key
			}
		}
	}
	out = jsonx.NewObject()
	for _, key := range []string{"host", "verdict", "target", "confidence", "reasons", "checked_at"} {
		out.Set(key, get(best, key, nil))
	}
	return out, nil
}
