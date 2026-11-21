package agents

import (
	"encoding/json"
	"math/big"
	"strings"

	"configflow/internal/taskstatus"
)

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

// CompareVersions is compare_versions(version1, version2): -1, 0 or 1. The
// "-suffix" is dropped, the dotted parts are padded with "0" and compared as
// ASCII decimal components through the shared counter codec, otherwise as strings.
func CompareVersions(version1, version2 string) int {
	v1 := strings.Split(strings.SplitN(version1, "-", 2)[0], ".")
	v2 := strings.Split(strings.SplitN(version2, "-", 2)[0], ".")
	for len(v1) < len(v2) {
		v1 = append(v1, "0")
	}
	for len(v2) < len(v1) {
		v2 = append(v2, "0")
	}
	for i := range v1 {
		n1, ok1 := versionComponentNumber(v1[i])
		n2, ok2 := versionComponentNumber(v2[i])
		if ok1 && ok2 {
			if c := n1.Cmp(n2); c != 0 {
				return c
			}
			continue
		}
		// int() failed on either side: compare the texts.
		if v1[i] < v2[i] {
			return -1
		} else if v1[i] > v2[i] {
			return 1
		}
	}
	return 0
}

// HasUpdate is has_update(current_version).
func HasUpdate(currentVersion string) bool {
	return CompareVersions(currentVersion, latestVersion) < 0
}

// versionComponentNumber delegates scalar handling to the shared native codec.
// Empty dotted components retain the original lexical fallback, rather than
// becoming counter zero. No Unicode integer grammar or interpreter digit cap
// is part of a machine-generated software version.
func versionComponentNumber(component string) (*big.Int, bool) {
	component = strings.TrimSpace(component)
	if component == "" {
		return nil, false
	}
	raw, err := json.Marshal(component)
	if err != nil {
		return nil, false
	}
	number, err := taskstatus.Count(raw)
	return number, err == nil
}
