package metrics

var zeroStats = func() map[string]any { return map[string]any{"avg": 0, "max": 0, "min": 0} }

func emptyTrafficStats(period string) map[string]any {
	return map[string]any{
		"bytes_sent": 0, "bytes_recv": 0, "bytes_sent_delta": 0, "bytes_recv_delta": 0,
		"avg_speed_sent": 0, "avg_speed_recv": 0, "period": period, "start_time": nil, "end_time": nil,
	}
}

func pyFloorDiv(a, b float64) float64 { return floorDiv(a, b) }
