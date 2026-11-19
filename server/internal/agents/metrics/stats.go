package metrics

import (
	"bytes"
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"time"

	"configflow/internal/jsondecode"
)

// Statistic is the finite average, maximum and minimum of available samples.
type Statistic struct {
	Avg json.RawMessage `json:"avg"`
	Max json.RawMessage `json:"max"`
	Min json.RawMessage `json:"min"`
}

// Summary contains the five monitored series for a requested window.
type Summary struct {
	CPU            Statistic `json:"cpu"`
	Memory         Statistic `json:"memory"`
	Disk           Statistic `json:"disk"`
	NetworkSent    Statistic `json:"network_sent"`
	NetworkRecv    Statistic `json:"network_recv"`
	DataPoints     int       `json:"data_points"`
	TimeRangeHours *big.Int  `json:"time_range_hours"`
}

// MarshalJSON keeps the established empty-object response when no point exists.
func (s Summary) MarshalJSON() ([]byte, error) {
	if s.DataPoints == 0 {
		return []byte(`{}`), nil
	}
	type summary Summary
	return json.Marshal(summary(s))
}

type sampleNumber struct {
	integer *big.Int
	real    float64
	raw     json.RawMessage
}

func parseSampleNumber(raw json.RawMessage) (sampleNumber, bool) {
	n := sampleNumber{raw: raw}
	if string(raw) == "true" {
		n.integer = big.NewInt(1)
		return n, true
	}
	if string(raw) == "false" {
		n.integer = new(big.Int)
		return n, true
	}
	if v, ok := integerJSON(raw); ok {
		f, _ := new(big.Float).SetInt(v).Float64()
		if math.IsInf(f, 0) {
			return n, false
		}
		n.integer = v
		return n, true
	}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) || raw[0] == '"' {
		return n, false
	}
	var value json.Number
	if jsondecode.Unmarshal(raw, &value) != nil {
		return n, false
	}
	f, err := value.Float64()
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return n, false
	}
	n.real = f
	return n, true
}
func (n sampleNumber) rat() *big.Rat {
	if n.integer != nil {
		return new(big.Rat).SetInt(n.integer)
	}
	return new(big.Rat).SetFloat64(n.real)
}
func (n sampleNumber) float() float64 {
	if n.integer == nil {
		return n.real
	}
	f, _ := new(big.Float).SetInt(n.integer).Float64()
	return f
}
func finiteJSON(f float64) json.RawMessage {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return json.RawMessage("null")
	}
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !bytes.ContainsAny([]byte(text), ".eE") {
		text += ".0"
	}
	return json.RawMessage(text)
}
func statistic(values []sampleNumber, seen bool) Statistic {
	if len(values) == 0 {
		v := json.RawMessage("0")
		if seen {
			v = []byte("null")
		}
		return Statistic{v, v, v}
	}
	total := new(big.Int)
	sum := float64(0)
	floating := false
	low, high := values[0], values[0]
	for _, value := range values {
		if value.rat().Cmp(low.rat()) < 0 {
			low = value
		}
		if value.rat().Cmp(high.rat()) > 0 {
			high = value
		}
		if !floating && value.integer != nil {
			total.Add(total, value.integer)
		} else {
			if !floating {
				sum, _ = new(big.Float).SetInt(total).Float64()
				floating = true
			}
			sum += value.float()
		}
	}
	if !floating {
		sum, _ = new(big.Rat).SetFrac(total, big.NewInt(int64(len(values)))).Float64()
	} else {
		sum /= float64(len(values))
	}
	return Statistic{finiteJSON(sum), high.raw, low.raw}
}
func sampleFields(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var fields map[string]json.RawMessage
	if jsondecode.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

// Summarize computes native statistics, isolating malformed stored components.
func (h *History) Summarize(id string, hours *big.Int) (*Summary, error) {
	samples, err := h.Samples(id, hours)
	if err != nil {
		return nil, err
	}
	summary := &Summary{TimeRangeHours: hours, DataPoints: len(samples)}
	var values [5][]sampleNumber
	var seen [5]bool
	for _, raw := range samples {
		fields, ok := sampleFields(raw)
		if !ok {
			h.warnCorrupt(id, "sample")
			continue
		}
		for i, field := range []struct{ section, key string }{{"cpu", "usage_percent"}, {"memory", "used_percent"}, {"disk", "used_percent"}, {"network", "speed_sent"}, {"network", "speed_recv"}} {
			section, ok := sampleFields(fields[field.section])
			if !ok {
				if raw, present := fields[field.section]; present && string(raw) != "null" {
					h.warnCorrupt(id, field.section)
				}
				continue
			}
			if raw, present := section[field.key]; present {
				seen[i] = true
				if value, ok := parseSampleNumber(raw); ok {
					values[i] = append(values[i], value)
				} else if string(raw) != "null" {
					h.warnCorrupt(id, field.section+"."+field.key)
				}
			}
		}
	}
	summary.CPU = statistic(values[0], seen[0])
	summary.Memory = statistic(values[1], seen[1])
	summary.Disk = statistic(values[2], seen[2])
	summary.NetworkSent = statistic(values[3], seen[3])
	summary.NetworkRecv = statistic(values[4], seen[4])
	return summary, nil
}

// TrafficStats is the cumulative traffic and reset-aware delta of a metrics window.
type TrafficStats struct {
	BytesSent      json.RawMessage `json:"bytes_sent"`
	BytesRecv      json.RawMessage `json:"bytes_recv"`
	BytesSentDelta json.RawMessage `json:"bytes_sent_delta"`
	BytesRecvDelta json.RawMessage `json:"bytes_recv_delta"`
	AvgSpeedSent   json.RawMessage `json:"avg_speed_sent"`
	AvgSpeedRecv   json.RawMessage `json:"avg_speed_recv"`
	Period         string          `json:"period"`
	StartTime      *string         `json:"start_time"`
	EndTime        *string         `json:"end_time"`
	DataPoints     *int            `json:"data_points,omitempty"`
}

// TrafficTrendPoint is a sample and optional reset-aware delta since the previous sample.
type TrafficTrendPoint struct {
	Timestamp string          `json:"timestamp"`
	BytesSent json.RawMessage `json:"bytes_sent"`
	BytesRecv json.RawMessage `json:"bytes_recv"`
	SentDelta json.RawMessage `json:"sent_delta,omitempty"`
	RecvDelta json.RawMessage `json:"recv_delta,omitempty"`
	SpeedSent json.RawMessage `json:"speed_sent"`
	SpeedRecv json.RawMessage `json:"speed_recv"`
}

type trafficSample struct {
	at                   time.Time
	sent, recv           sampleNumber
	speedSent, speedRecv json.RawMessage
}

func (h *History) trafficSamples(id string, samples []json.RawMessage) []trafficSample {
	result := make([]trafficSample, 0, len(samples))
	for _, raw := range samples {
		fields, ok := sampleFields(raw)
		if !ok {
			continue
		}
		var stamp string
		if jsondecode.Unmarshal(fields["timestamp"], &stamp) != nil {
			continue
		}
		at, aware, ok := parseNaiveISO(stamp)
		if !ok || aware {
			continue
		}
		network, present := fields["network"]
		var values map[string]json.RawMessage
		if present {
			values, ok = sampleFields(network)
			if !ok {
				if string(network) != "null" {
					h.warnCorrupt(id, "network")
				}
				continue
			}
		}
		counter := func(key string) (sampleNumber, bool) {
			raw, exists := values[key]
			if !exists {
				raw = []byte("0")
			}
			return parseSampleNumber(raw)
		}
		sent, ok := counter("bytes_sent")
		if !ok {
			h.warnCorrupt(id, "network.bytes_sent")
			continue
		}
		recv, ok := counter("bytes_recv")
		if !ok {
			h.warnCorrupt(id, "network.bytes_recv")
			continue
		}
		speed := func(key string) json.RawMessage {
			raw, exists := values[key]
			if !exists {
				return json.RawMessage("0")
			}
			if value, ok := parseSampleNumber(raw); ok {
				return value.raw
			}
			return json.RawMessage("null")
		}
		result = append(result, trafficSample{at, sent, recv, speed("speed_sent"), speed("speed_recv")})
	}
	return result
}

// secondsBetween avoids time.Duration saturation for old persisted timestamps.
func secondsBetween(later, earlier time.Time) float64 {
	micros := (later.Unix()-earlier.Unix())*1_000_000 + int64(later.Nanosecond()/1000-earlier.Nanosecond()/1000)
	f, _ := new(big.Rat).SetFrac(big.NewInt(micros), big.NewInt(1_000_000)).Float64()
	return f
}
func delta(after, before sampleNumber) sampleNumber {
	if after.rat().Cmp(before.rat()) < 0 {
		return after
	}
	if after.integer != nil && before.integer != nil {
		n := new(big.Int).Sub(after.integer, before.integer)
		return sampleNumber{integer: n, raw: json.RawMessage(n.String())}
	}
	f := after.float() - before.float()
	return sampleNumber{real: f, raw: finiteJSON(f)}
}
func speedDelta(delta sampleNumber, duration float64) json.RawMessage {
	if duration <= 0 {
		return json.RawMessage("0")
	}
	return finiteJSON(delta.float() / duration)
}

// TrafficStatistics returns native traffic totals; corrupt stored points are skipped.
func (h *History) TrafficStatistics(id, period string) (*TrafficStats, error) {
	raw, err := h.Samples(id, big.NewInt(24))
	if err != nil {
		return nil, err
	}
	samples := h.trafficSamples(id, raw)
	current := naiveNow()
	start := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, time.UTC)
	if period == "week" {
		start = start.AddDate(0, 0, -((int(current.Weekday()) + 6) % 7))
	}
	if period == "today" || period == "week" {
		kept := samples[:0]
		for _, sample := range samples {
			if !sample.at.Before(start) {
				kept = append(kept, sample)
			}
		}
		samples = kept
	}
	zero := json.RawMessage("0")
	out := &TrafficStats{BytesSent: zero, BytesRecv: zero, BytesSentDelta: zero, BytesRecvDelta: zero, AvgSpeedSent: zero, AvgSpeedRecv: zero, Period: period}
	if len(samples) == 0 {
		return out, nil
	}
	first, last := samples[0], samples[len(samples)-1]
	sent, recv := delta(last.sent, first.sent), delta(last.recv, first.recv)
	duration := secondsBetween(last.at, first.at)
	startText, endText, count := isoformat(first.at), isoformat(last.at), len(samples)
	out.BytesSent = last.sent.raw
	out.BytesRecv = last.recv.raw
	out.BytesSentDelta = sent.raw
	out.BytesRecvDelta = recv.raw
	out.AvgSpeedSent = speedDelta(sent, duration)
	out.AvgSpeedRecv = speedDelta(recv, duration)
	out.StartTime = &startText
	out.EndTime = &endText
	out.DataPoints = &count
	return out, nil
}

// TrafficTrend returns interval deltas plus the final raw network sample.
func (h *History) TrafficTrend(id string, hours, intervalMinutes *big.Int) ([]TrafficTrendPoint, error) {
	raw, err := h.Samples(id, hours)
	if err != nil {
		return nil, err
	}
	samples := h.trafficSamples(id, raw)
	out := []TrafficTrendPoint{}
	if len(samples) == 0 {
		return out, nil
	}
	interval, _ := new(big.Float).SetInt(new(big.Int).Mul(intervalMinutes, big.NewInt(60))).Float64()
	previous, groupStart := samples[0], samples[0].at
	for _, sample := range samples[1:] {
		if secondsBetween(sample.at, groupStart) >= interval {
			sent, recv := delta(sample.sent, previous.sent), delta(sample.recv, previous.recv)
			duration := secondsBetween(sample.at, previous.at)
			out = append(out, TrafficTrendPoint{isoformat(sample.at), sample.sent.raw, sample.recv.raw, sent.raw, recv.raw, speedDelta(sent, duration), speedDelta(recv, duration)})
			groupStart = sample.at
		}
		previous = sample
	}
	out = append(out, TrafficTrendPoint{Timestamp: isoformat(previous.at), BytesSent: previous.sent.raw, BytesRecv: previous.recv.raw, SpeedSent: previous.speedSent, SpeedRecv: previous.speedRecv})
	return out, nil
}
