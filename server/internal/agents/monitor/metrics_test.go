package monitor

// Port of backend/test_oxidns_metrics.py: dashboard numbers must preserve
// unknowns and the real denominators.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

func cacheText(tag string, lookups, fresh, stale any, extra ...any) string {
	misses, expired, entries := any(0), any(0), any(0)
	if len(extra) > 0 {
		misses = extra[0]
	}
	if len(extra) > 1 {
		expired = extra[1]
	}
	if len(extra) > 2 {
		entries = extra[2]
	}
	return strings.Join([]string{
		fmt.Sprintf(`cache_lookup_total{plugin_tag="%s"} %v`, tag, lookups),
		fmt.Sprintf(`cache_hit_total{plugin_tag="%s",kind="fresh"} %v`, tag, fresh),
		fmt.Sprintf(`cache_hit_total{plugin_tag="%s",kind="stale"} %v`, tag, stale),
		fmt.Sprintf(`cache_miss_total{plugin_tag="%s"} %v`, tag, misses),
		fmt.Sprintf(`cache_expired_total{plugin_tag="%s"} %v`, tag, expired),
		fmt.Sprintf(`cache_entry_count{plugin_tag="%s"} %v`, tag, entries),
	}, "\n")
}

func serverText(tag, protocol string, requests, failed, elapsed, count any, inflight ...any) string {
	var in any = 0
	if len(inflight) > 0 {
		in = inflight[0]
	}
	values := []struct {
		field string
		value any
	}{{"request_total", requests}, {"failed_total", failed}, {"latency_sum_ms", elapsed}, {"latency_count", count}, {"inflight", in}}
	var lines []string
	for _, v := range values {
		lines = append(lines, fmt.Sprintf(`server_%s{plugin_tag="%s",protocol="%s"} %v`, v.field, tag, protocol, v.value))
	}
	return strings.Join(lines, "\n")
}

func upstreamText(tag, name string, requests, failed, elapsed, count any) string {
	values := []struct {
		field string
		value any
	}{{"query_total", requests}, {"error_total", failed}, {"latency_sum_ms", elapsed}, {"latency_count", count}}
	var lines []string
	for _, v := range values {
		lines = append(lines, fmt.Sprintf(`forward_upstream_%s{plugin_tag="%s",upstream="%s"} %v`, v.field, tag, name, v.value))
	}
	return strings.Join(lines, "\n")
}

func summarize(t *testing.T, text string) *jsonx.Object {
	t.Helper()
	result, err := SummarizeMetrics(text)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func parse(t *testing.T, text string) *jsonx.Object {
	t.Helper()
	result, err := ParseMetrics(text)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func wantJSON(t *testing.T, got, want any) {
	t.Helper()
	if !jsonx.Equal(got, want) {
		t.Fatalf("got %s\nwant %s", jsonx.Repr(got), jsonx.Repr(want))
	}
}

func approx(t *testing.T, got any, want float64) {
	t.Helper()
	n, ok := got.(jsonx.Number)
	if !ok || math.Abs(n.Float64()-want) > 1e-6*math.Max(1, math.Abs(want)) {
		t.Fatalf("got %s want ≈ %v", jsonx.Repr(got), want)
	}
}

func metricsOf(o *jsonx.Object) *jsonx.Object { return o.GetObject("metrics") }

func row(o *jsonx.Object, kind string, i int) *jsonx.Object {
	return o.GetList(kind)[i].(*jsonx.Object)
}

func TestMultipleCachesUseLookupWeightedRateIncludingExpired(t *testing.T) {
	result := summarize(t, cacheText("large", 100, 60, 10, 20, 10, 40)+"\n"+cacheText("small", 10, 10, 0, 0, 0, 5))
	approx(t, metricsOf(result).Value("cache_hit_rate"), 100*80.0/110)
	wantJSON(t, row(result, "caches", 0), jsonx.Obj("tag", "large", "lookups", 100, "hits", 70, "fresh_hits", 60,
		"stale_hits", 10, "misses", 20, "expired", 10, "entries", 40, "hit_rate", 70))
	wantJSON(t, row(result, "caches", 1).Value("hit_rate"), 100)
	wantJSON(t, metricsOf(result).Value("requests_total"), nil)
}

func TestServerTotalsAndWeightedLatencyArePerListener(t *testing.T) {
	result := summarize(t, strings.Join([]string{
		serverText("dns", "udp", 100, 2, 1000, 100, 3),
		serverText("dns", "tcp", 10, 1, 1000, 10, 1),
		`query_total{plugin_tag="collector"} 999`,
		`forward_query_total{plugin_tag="upstream"} 999`,
	}, "\n"))
	m := metricsOf(result)
	wantJSON(t, pick(m, "requests_total", "failed_total", "inflight", "cache_hit_rate"),
		jsonx.Obj("requests_total", 110, "failed_total", 3, "inflight", 4, "cache_hit_rate", nil))
	approx(t, m.Value("avg_latency_ms"), 2000.0/110)
	wantJSON(t, []any{row(result, "servers", 0).Value("protocol"), row(result, "servers", 0).Value("avg_latency_ms"),
		row(result, "servers", 1).Value("protocol"), row(result, "servers", 1).Value("avg_latency_ms")},
		[]any{"tcp", 100, "udp", 10})
}

func TestZeroObservationsAreKnownButZeroDenominatorsAreUnknown(t *testing.T) {
	result := summarize(t, cacheText("c", 0, 0, 0)+"\n"+serverText("s", "udp", 0, 0, 0, 0))
	wantJSON(t, metricsOf(result), jsonx.Obj("requests_total", 0, "failed_total", 0, "inflight", 0,
		"avg_latency_ms", nil, "cache_hit_rate", nil))
	wantJSON(t, row(result, "caches", 0).Value("hits"), 0)
	wantJSON(t, row(result, "caches", 0).Value("hit_rate"), nil)
}

func TestEmptyOrUnrelatedInputDoesNotFabricateZeroes(t *testing.T) {
	for _, text := range []string{"", "# TYPE cache_hit_total counter\n", "unrelated_total 500"} {
		result := summarize(t, text)
		for _, v := range metricsOf(result).Values() {
			if v != nil {
				t.Fatal(text, jsonx.Repr(result))
			}
		}
		if len(result.GetList("servers")) != 0 || len(result.GetList("caches")) != 0 {
			t.Fatal(text)
		}
	}
}

func TestPartialCacheAndListenerMakeRelatedGlobalTotalsUnknown(t *testing.T) {
	result := summarize(t, strings.Join([]string{
		cacheText("complete", 20, 10, 0),
		`cache_hit_total{plugin_tag="partial",kind="fresh"} 4`,
		serverText("complete", "udp", 10, 0, 20, 10),
		`server_inflight{plugin_tag="partial",protocol="tcp"} 0`,
	}, "\n"))
	partial := row(result, "caches", 1)
	wantJSON(t, pick(partial, "fresh_hits", "stale_hits", "hits", "lookups"),
		jsonx.Obj("fresh_hits", 4, "stale_hits", nil, "hits", nil, "lookups", nil))
	m := metricsOf(result)
	wantJSON(t, pick(m, "cache_hit_rate", "requests_total", "avg_latency_ms", "inflight"),
		jsonx.Obj("cache_hit_rate", nil, "requests_total", nil, "avg_latency_ms", nil, "inflight", 0))
}

func TestInvalidValuesAreUnknownNotZero(t *testing.T) {
	for _, bad := range []string{"NaN", "+Inf", "-Inf", "-1", "1e999", "not_a_number", "1_000"} {
		result := summarize(t, cacheText("c", 10, bad, 0)+"\n"+serverText("s", "udp", bad, 0, 1, 1))
		if row(result, "caches", 0).Value("fresh_hits") != nil || row(result, "caches", 0).Value("hits") != nil ||
			metricsOf(result).Value("cache_hit_rate") != nil || metricsOf(result).Value("requests_total") != nil {
			t.Fatal(bad, jsonx.Repr(result))
		}
	}
}

func TestContradictoryHitCountNeverExceeds100Percent(t *testing.T) {
	result := summarize(t, cacheText("c", 10, 12, 1))
	wantJSON(t, row(result, "caches", 0).Value("hits"), 13)
	wantJSON(t, row(result, "caches", 0).Value("hit_rate"), nil)
	wantJSON(t, metricsOf(result).Value("cache_hit_rate"), nil)
}

func TestLabelsUsePrometheusEscapingAndAllowCommasBracesAndTimestamp(t *testing.T) {
	text := `cache_entry_count{plugin_tag="a\"b\\c\nd,{x}",ignored="value,with=punctuation"} 7 1792100000000`
	result := summarize(t, text)
	wantJSON(t, row(result, "caches", 0).Value("tag"), "a\"b\\c\nd,{x}")
	wantJSON(t, row(result, "caches", 0).Value("entries"), 7)
}

func TestPrefixedAliasesDuplicatesAndConflictsDoNotDoubleCount(t *testing.T) {
	text := serverText("s", "udp", 12, 1, 24, 12)
	var prefixedLines []string
	for _, line := range strings.Split(text, "\n") {
		prefixedLines = append(prefixedLines, "oxidns_"+line)
	}
	prefixed := strings.Join(prefixedLines, "\n")
	wantJSON(t, summarize(t, text), summarize(t, prefixed))
	wantJSON(t, metricsOf(summarize(t, text+"\n"+prefixed)).Value("requests_total"), 12)
	conflict := "\noxidns_server_request_total{protocol=\"udp\",plugin_tag=\"s\"} 13"
	wantJSON(t, metricsOf(summarize(t, text+conflict)).Value("requests_total"), nil)
}

func TestMalformedOrUnusableSamplesAreSkipped(t *testing.T) {
	for _, line := range []string{
		`cache_entry_count{plugin_tag="broken} 1`,
		`cache_entry_count{plugin_tag="bad\q"} 1`,
		`cache_entry_count{plugin_tag="a",plugin_tag="b"} 1`,
		`cache_entry_count{plugin_tag="a" trailing} 1`,
		`cache_entry_count{} 1`,
		`cache_entry_count 1`,
		`server_request_total{plugin_tag="a"} 1`,
		`cache_hit_total{plugin_tag="a",kind="other"} 1`,
		`cache_entry_count{plugin_tag="a"} 1 extra invalid`,
	} {
		result := summarize(t, line)
		if len(result.GetList("caches")) != 0 || len(result.GetList("servers")) != 0 {
			t.Fatal(line)
		}
	}
}

func TestScientificNotationAndLargeIntegerCounter(t *testing.T) {
	result := summarize(t, strings.Join([]string{
		`server_request_total{plugin_tag="s",protocol="udp"} 18446744073709551615`,
		`server_latency_count{plugin_tag="s",protocol="udp"} 2e2`,
		`server_latency_sum_ms{plugin_tag="s",protocol="udp"} 3.0e3`,
	}, "\n"))
	if metricsOf(result).Value("requests_total").(jsonx.Number).String() != "18446744073709551615" {
		t.Fatal(jsonx.Repr(result))
	}
	wantJSON(t, metricsOf(result).Value("avg_latency_ms"), 15)
}

// test_nontext_input_is_rejected: ParseMetrics takes a Go string; the str
// check of Python is the caller's (overviewSnapshot rejects a non-string
// "metrics" with INVALID_RESPONSE, TestOverviewRejectsNonTextMetrics).

func TestInputLimitsRejectInsteadOfReturningTruncatedTotals(t *testing.T) {
	restore := func(input, samples, line int) func() {
		a, b, c := MaxInputBytes, MaxSamples, MaxLineLength
		MaxInputBytes, MaxSamples, MaxLineLength = input, samples, line
		return func() { MaxInputBytes, MaxSamples, MaxLineLength = a, b, c }
	}
	want := func(text, message string) {
		t.Helper()
		_, err := SummarizeMetrics(text)
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("%q: %v", text, err)
		}
	}
	undo := restore(20, MaxSamples, MaxLineLength)
	want(strings.Repeat("x", 21), "size limit")
	want(strings.Repeat("界", 7), "size limit")
	undo()
	undo = restore(1024, 1, MaxLineLength)
	want("unknown 1\nunknown 2", "sample limit")
	undo()
	undo = restore(1024, 1, 8)
	want(strings.Repeat("#", 9), "line exceeds")
	undo()
}

func TestInconsistentCacheCannotBeHiddenByAnotherCacheDenominator(t *testing.T) {
	result := summarize(t, cacheText("bad", 10, 12, 1)+"\n"+cacheText("good", 100, 20, 0))
	wantJSON(t, row(result, "caches", 1).Value("hit_rate"), 20)
	wantJSON(t, metricsOf(result).Value("cache_hit_rate"), nil)
}

func TestNonfiniteAggregateIsUnknown(t *testing.T) {
	result := summarize(t, serverText("a", "udp", "1e308", 0, "1e308", 1)+"\n"+serverText("b", "tcp", "1e308", 0, "1e308", 1))
	wantJSON(t, metricsOf(result).Value("requests_total"), nil)
	wantJSON(t, metricsOf(result).Value("avg_latency_ms"), nil)
}

func TestUpstreamsKeepAttemptsSeparateFromListenerRequests(t *testing.T) {
	text := strings.Join([]string{
		serverText("dns", "udp", 8, 0, 40, 8),
		upstreamText("remote", "https://dns.example/dns-query", 10, 2, 120, 6),
		upstreamText("local", "udp://192.0.2.1", 5, 0, 25, 5),
		`forward_upstream_timeout_total{plugin_tag="remote",upstream="https://dns.example/dns-query"} 2`,
	}, "\n")
	result := summarize(t, text)
	wantJSON(t, metricsOf(result).Value("requests_total"), 8)
	wantJSON(t, result.Value("upstreams"), []any{
		jsonx.Obj("tag", "remote", "upstream", "https://dns.example/dns-query", "requests", 10, "failed", 2, "avg_latency_ms", 20),
		jsonx.Obj("tag", "local", "upstream", "udp://192.0.2.1", "requests", 5, "failed", 0, "avg_latency_ms", 5),
	})
	wantJSON(t, summarize(t, text+"\n"+text).Value("upstreams"), result.Value("upstreams"))
}

func TestResetUsesRawWeightedDifferencesAndKeepsLiveGauges(t *testing.T) {
	baseline := parse(t, strings.Join([]string{
		serverText("dns", "udp", 100, 1, 1000, 100, 2),
		cacheText("cache", 100, 50, 10, 30, 10, 99),
		upstreamText("forward", "dns", 50, 5, 5000, 50),
	}, "\n"))
	encoded, _ := jsonx.Compact(baseline) // identical after persistence
	persisted, _ := jsonx.Parse(encoded)
	baseline = persisted.(*jsonx.Object)
	current := parse(t, strings.Join([]string{
		serverText("dns", "udp", 110, 3, 1300, 110, 4),
		cacheText("cache", 120, 65, 12, 32, 11, 105),
		upstreamText("forward", "dns", 54, 6, 5800, 54),
	}, "\n"))
	original := current.DeepCopy()
	delta, partial, err := ApplyMetricsBaseline(current, baseline)
	if err != nil || partial {
		t.Fatal(err, partial)
	}
	result := SummarizeObservations(delta)
	wantJSON(t, current, original)
	wantJSON(t, metricsOf(result), jsonx.Obj("requests_total", 10, "failed_total", 2, "avg_latency_ms", 30,
		"inflight", 4, "cache_hit_rate", 85))
	wantJSON(t, row(result, "caches", 0).Value("entries"), 105)
	wantJSON(t, pick(row(result, "upstreams", 0), "requests", "avg_latency_ms", "failed"),
		jsonx.Obj("requests", 4, "avg_latency_ms", 200, "failed", 1))
}

func TestResetToSameSampleHasZeroCountersAndUnknownRatios(t *testing.T) {
	snapshot := parse(t, serverText("s", "udp", 100, 0, 1000, 100, 7)+"\n"+cacheText("c", 5, 2, 1, 0, 0, 20))
	adjusted, partial, _ := ApplyMetricsBaseline(snapshot, snapshot)
	result := SummarizeObservations(adjusted)
	if partial {
		t.Fatal("partial")
	}
	wantJSON(t, pick(metricsOf(result), "requests_total", "avg_latency_ms", "cache_hit_rate", "inflight"),
		jsonx.Obj("requests_total", 0, "avg_latency_ms", nil, "cache_hit_rate", nil, "inflight", 7))
	wantJSON(t, row(result, "caches", 0).Value("entries"), 20)
}

func TestNewOrMissingSeriesArePartialNotFabricatedFromZero(t *testing.T) {
	baseline := parse(t, serverText("old", "udp", 100, 2, 500, 100))
	current := parse(t, serverText("new", "udp", 5, 1, 20, 5, 2))
	adjusted, partial, _ := ApplyMetricsBaseline(current, baseline)
	result := SummarizeObservations(adjusted)
	if !partial {
		t.Fatal("not partial")
	}
	wantJSON(t, pick(metricsOf(result), "requests_total", "avg_latency_ms", "inflight"),
		jsonx.Obj("requests_total", nil, "avg_latency_ms", nil, "inflight", 2))
}

func TestPluginCounterResetReportsNewLifetimeInsteadOfNegative(t *testing.T) {
	baseline := parse(t, upstreamText("f", "dns", 100, 5, 1000, 100))
	current := parse(t, upstreamText("f", "dns", 3, 0, 15, 3))
	adjusted, partial, _ := ApplyMetricsBaseline(current, baseline)
	result := SummarizeObservations(adjusted)
	if !partial {
		t.Fatal("not partial")
	}
	wantJSON(t, pick(row(result, "upstreams", 0), "requests", "avg_latency_ms"), jsonx.Obj("requests", 3, "avg_latency_ms", 5))
}

func TestMissingLatencyCounterDoesNotBecomeAverageOfAverages(t *testing.T) {
	baseline := parse(t, upstreamText("f", "dns", 20, 2, "NaN", 18))
	current := parse(t, upstreamText("f", "dns", 25, 3, 1000, 22))
	adjusted, partial, _ := ApplyMetricsBaseline(current, baseline)
	if !partial {
		t.Fatal("not partial")
	}
	result := SummarizeObservations(adjusted)
	wantJSON(t, pick(row(result, "upstreams", 0), "requests", "avg_latency_ms"), jsonx.Obj("requests", 5, "avg_latency_ms", nil))
}

func TestUpstreamIdentityIsGroupAndNameNotNameAlone(t *testing.T) {
	result := summarize(t, strings.Join([]string{
		upstreamText("fallback", "dns", 1, 0, 10, 1),
		upstreamText("remote", "dns", 3, 1, 12, 2),
		`forward_upstream_query_total{plugin_tag="missing_name"} 99`,
	}, "\n"))
	if len(result.GetList("upstreams")) != 2 {
		t.Fatal(jsonx.Repr(result))
	}
	wantJSON(t, []any{row(result, "upstreams", 0).Value("requests"), row(result, "upstreams", 1).Value("requests")}, []any{3, 1})
	wantJSON(t, []any{row(result, "upstreams", 0).Value("avg_latency_ms"), row(result, "upstreams", 1).Value("avg_latency_ms")}, []any{6, 10})
}

func TestCounterResetDetectionExcludesGaugesAndMissingSeries(t *testing.T) {
	baseline := parse(t, serverText("s", "udp", 100, 0, 1000, 100, 7)+"\n"+cacheText("c", 5, 2, 1, 0, 0, 20))
	gaugesChanged := parse(t, serverText("s", "udp", 101, 0, 1010, 101, 0)+"\n"+cacheText("c", 5, 2, 1, 0, 0, 0))
	if MetricsCountersRestarted(gaugesChanged, baseline) || MetricsCountersRestarted(parse(t, ""), baseline) {
		t.Fatal("restart")
	}
	if !MetricsCountersRestarted(parse(t, serverText("s", "udp", 1, 0, 10, 1)), baseline) {
		t.Fatal("no restart")
	}
}
