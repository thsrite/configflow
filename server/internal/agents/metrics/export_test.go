package metrics

// SetTrafficNow replaces the clock of the traffic history and returns a
// function restoring it (tests).
func SetTrafficNow(fn func() float64) func() {
	previous := trafficNow
	trafficNow = fn
	return func() { trafficNow = previous }
}
