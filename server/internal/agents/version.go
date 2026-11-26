package agents

import "configflow/internal/version"

// ChangelogEntry is one VERSION_CHANGELOG item.
type ChangelogEntry struct {
	Version  string
	Date     string
	Features []string
}

// Changelog is VERSION_CHANGELOG: every entry, newest first (the dict's
// declaration order).
func Changelog() []ChangelogEntry {
	out := make([]ChangelogEntry, len(versionChangelog))
	copy(out, versionChangelog)
	return out
}

// VersionChangelog is VERSION_CHANGELOG.get(version).
func VersionChangelog(version string) (ChangelogEntry, bool) {
	for _, entry := range versionChangelog {
		if entry.Version == version {
			return entry, true
		}
	}
	return ChangelogEntry{}, false
}

// latestVersion is the release GetLatestVersion reports: LatestAgentVersion,
// except while a Flask differential pins Python's reference value.
var latestVersion = LatestAgentVersion

// GetLatestVersion is get_latest_version().
func GetLatestVersion() string { return latestVersion }

// HasUpdate is has_update(current_version).
func HasUpdate(currentVersion string) bool {
	return version.CompareAgent(currentVersion, latestVersion) < 0
}
