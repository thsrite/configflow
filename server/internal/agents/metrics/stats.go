package metrics

// get_metrics_summary, get_traffic_stats and get_traffic_trend of
// metrics_history.py. The traffic functions are used by the traffic routes
// (A05).

import (
	"math/big"
	"time"

	"configflow/internal/jsonx"
)

var zeroStats = func() *jsonx.Object { return jsonx.Obj("avg", 0, "max", 0, "min", 0) }

// calcStats is calc_stats(values).
func calcStats(values []any) (*jsonx.Object, error) {
	if len(values) == 0 {
		return zeroStats(), nil
	}
	avg, err := pyMean(values)
	if err != nil {
		return nil, err
	}
	high, err := pyMax(values)
	if err != nil {
		return nil, err
	}
	low, err := pyMin(values)
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("avg", avg, "max", high, "min", low), nil
}

// GetMetricsSummary is get_metrics_summary(agent_id, hours): average,
// maximum and minimum of the last hours hours; an empty object without
// points. Errors are Python's (OverflowError for hours, TypeError for
// values that are not numbers).
func (h *History) GetMetricsSummary(agentID string, hours *big.Int) (*jsonx.Object, error) {
	metrics, err := h.GetMetrics(agentID, hours)
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		return jsonx.NewObject(), nil
	}
	var cpuValues, memoryValues, diskValues, sentValues, recvValues []any
	collect := func(section any, key string, into *[]any) error {
		found, err := pyContains(section, key)
		if err != nil || !found {
			return err
		}
		value, err := pyIndex(section, key)
		if err != nil {
			return err
		}
		*into = append(*into, value)
		return nil
	}
	for _, item := range metrics {
		metric := item.(*jsonx.Object)
		section := func(key string) any {
			value, _ := pyGet(metric, key, jsonx.NewObject())
			return value
		}
		cpu, memory, disk, network := section("cpu"), section("memory"), section("disk"), section("network")
		for _, step := range []struct {
			section any
			key     string
			into    *[]any
		}{{cpu, "usage_percent", &cpuValues}, {memory, "used_percent", &memoryValues}, {disk, "used_percent", &diskValues},
			{network, "speed_sent", &sentValues}, {network, "speed_recv", &recvValues}} {
			if err := collect(step.section, step.key, step.into); err != nil {
				return nil, err
			}
		}
	}
	summary := jsonx.NewObject()
	for _, step := range []struct {
		key    string
		values []any
	}{{"cpu", cpuValues}, {"memory", memoryValues}, {"disk", diskValues}, {"network_sent", sentValues}, {"network_recv", recvValues}} {
		stats, err := calcStats(step.values)
		if err != nil {
			return nil, err
		}
		summary.Set(step.key, stats)
	}
	summary.Set("data_points", len(metrics))
	summary.Set("time_range_hours", jsonx.BigInt(hours))
	return summary, nil
}

func emptyTrafficStats(period string) *jsonx.Object {
	return jsonx.Obj("bytes_sent", 0, "bytes_recv", 0, "bytes_sent_delta", 0, "bytes_recv_delta", 0,
		"avg_speed_sent", 0, "avg_speed_recv", 0, "period", period, "start_time", nil, "end_time", nil)
}

// pointTime is datetime.fromisoformat(metric['timestamp']) of a point in
// memory (always a valid naive timestamp).
func pointTime(metric *jsonx.Object) time.Time {
	t, _, _ := parseNaiveISO(metric.GetString("timestamp"))
	return t
}

// totalSeconds is timedelta.total_seconds().
func totalSeconds(d time.Duration) float64 {
	micros := d.Microseconds()
	return float64(micros) / 1e6
}

// GetTrafficStats is get_traffic_stats(agent_id, period): cumulative
// traffic, deltas and average speeds of the period ("today", "week",
// "hours_24", anything else = "total") within the last 24 hours.
func (h *History) GetTrafficStats(agentID, period string) (*jsonx.Object, error) {
	metrics, err := h.GetMetrics(agentID, big.NewInt(24))
	if err != nil {
		return nil, err
	}
	if len(metrics) == 0 {
		return emptyTrafficStats(period), nil
	}
	current := naiveNow()
	var filtered []*jsonx.Object
	switch period {
	case "today", "week":
		start := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, time.UTC)
		if period == "week" {
			start = start.AddDate(0, 0, -((int(current.Weekday()) + 6) % 7))
		}
		for _, item := range metrics {
			metric := item.(*jsonx.Object)
			if !pointTime(metric).Before(start) {
				filtered = append(filtered, metric)
			}
		}
	default:
		for _, item := range metrics {
			filtered = append(filtered, item.(*jsonx.Object))
		}
	}
	if len(filtered) == 0 {
		return emptyTrafficStats(period), nil
	}
	latest, first := filtered[len(filtered)-1], filtered[0]
	values := [4]any{}
	for i, step := range []struct {
		metric *jsonx.Object
		key    string
	}{{latest, "bytes_sent"}, {latest, "bytes_recv"}, {first, "bytes_sent"}, {first, "bytes_recv"}} {
		network, _ := pyGet(step.metric, "network", jsonx.NewObject())
		value, err := pyGet(network, step.key, jsonx.Int(0))
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	currentSent, currentRecv, startSent, startRecv := values[0], values[1], values[2], values[3]
	sentDelta, err := pySub(currentSent, startSent)
	if err != nil {
		return nil, err
	}
	recvDelta, err := pySub(currentRecv, startRecv)
	if err != nil {
		return nil, err
	}
	// 如果差值为负，说明发生了重置，使用当前值作为增量
	if negative, err := pyLess(sentDelta, jsonx.Int(0)); err != nil {
		return nil, err
	} else if negative {
		sentDelta = currentSent
	}
	if negative, err := pyLess(recvDelta, jsonx.Int(0)); err != nil {
		return nil, err
	} else if negative {
		recvDelta = currentRecv
	}
	startTime, endTime := pointTime(first), pointTime(latest)
	duration := totalSeconds(endTime.Sub(startTime))
	var avgSent, avgRecv any = jsonx.Int(0), jsonx.Int(0)
	if duration > 0 {
		if avgSent, err = pyTrueDivFloat(sentDelta, duration); err != nil {
			return nil, err
		}
		if avgRecv, err = pyTrueDivFloat(recvDelta, duration); err != nil {
			return nil, err
		}
	}
	return jsonx.Obj("bytes_sent", currentSent, "bytes_recv", currentRecv, "bytes_sent_delta", sentDelta,
		"bytes_recv_delta", recvDelta, "avg_speed_sent", avgSent, "avg_speed_recv", avgRecv, "period", period,
		"start_time", isoformat(startTime), "end_time", isoformat(endTime), "data_points", len(filtered)), nil
}

// GetTrafficTrend is get_traffic_trend(agent_id, hours, interval_minutes):
// one point per interval with the traffic delta and average speed since
// the previous point, then the last point.
func (h *History) GetTrafficTrend(agentID string, hours, intervalMinutes *big.Int) ([]any, error) {
	metrics, err := h.GetMetrics(agentID, hours)
	if err != nil {
		return nil, err
	}
	trend := []any{}
	if len(metrics) == 0 {
		return trend, nil
	}
	intervalSeconds := jsonx.BigInt(new(big.Int).Mul(intervalMinutes, big.NewInt(60)))
	var prevMetric *jsonx.Object
	var prevTime, groupStartTime time.Time
	get := func(section any, key string) (any, error) { return pyGet(section, key, jsonx.Int(0)) }
	for _, item := range metrics {
		metric := item.(*jsonx.Object)
		timestamp := pointTime(metric)
		network, _ := pyGet(metric, "network", jsonx.NewObject())
		if prevMetric == nil {
			prevMetric, prevTime, groupStartTime = metric, timestamp, timestamp
			continue
		}
		timeDiff := totalSeconds(timestamp.Sub(groupStartTime))
		due, err := pyGreaterEqual(jsonx.Float(timeDiff), intervalSeconds)
		if err != nil {
			return nil, err
		}
		if due {
			prevNetwork, _ := pyGet(prevMetric, "network", jsonx.NewObject())
			delta := func(key string) (any, error) {
				after, err := get(network, key)
				if err != nil {
					return nil, err
				}
				before, err := get(prevNetwork, key)
				if err != nil {
					return nil, err
				}
				return pySub(after, before)
			}
			sentDelta, err := delta("bytes_sent")
			if err != nil {
				return nil, err
			}
			recvDelta, err := delta("bytes_recv")
			if err != nil {
				return nil, err
			}
			// 处理计数器重置
			for _, step := range []struct {
				value *any
				key   string
			}{{&sentDelta, "bytes_sent"}, {&recvDelta, "bytes_recv"}} {
				negative, err := pyLess(*step.value, jsonx.Int(0))
				if err != nil {
					return nil, err
				}
				if negative {
					if *step.value, err = get(network, step.key); err != nil {
						return nil, err
					}
				}
			}
			duration := totalSeconds(timestamp.Sub(prevTime))
			var speedSent, speedRecv any = jsonx.Int(0), jsonx.Int(0)
			if duration > 0 {
				if speedSent, err = pyTrueDivFloat(sentDelta, duration); err != nil {
					return nil, err
				}
				if speedRecv, err = pyTrueDivFloat(recvDelta, duration); err != nil {
					return nil, err
				}
			}
			bytesSent, err := get(network, "bytes_sent")
			if err != nil {
				return nil, err
			}
			bytesRecv, err := get(network, "bytes_recv")
			if err != nil {
				return nil, err
			}
			trend = append(trend, jsonx.Obj("timestamp", isoformat(timestamp), "bytes_sent", bytesSent,
				"bytes_recv", bytesRecv, "sent_delta", sentDelta, "recv_delta", recvDelta,
				"speed_sent", speedSent, "speed_recv", speedRecv))
			groupStartTime = timestamp
		}
		prevMetric, prevTime = metric, timestamp
	}
	network, _ := pyGet(prevMetric, "network", jsonx.NewObject())
	last := jsonx.Obj("timestamp", isoformat(prevTime))
	for _, key := range []string{"bytes_sent", "bytes_recv", "speed_sent", "speed_recv"} {
		value, err := get(network, key)
		if err != nil {
			return nil, err
		}
		last.Set(key, value)
	}
	return append(trend, last), nil
}
