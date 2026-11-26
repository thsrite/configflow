package agents

// SetLatestVersionForTest makes GetLatestVersion and HasUpdate use version
// until the returned function restores LatestAgentVersion. Flask differentials
// pin Python's LATEST_AGENT_VERSION (1.8.61-go) so both sides compare the same
// upgrade target; Go releases newer Agents (1.9.0-go and later) on its own.
func SetLatestVersionForTest(version string) (restore func()) {
	previous := latestVersion
	latestVersion = version
	return func() { latestVersion = previous }
}
