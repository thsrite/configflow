package agents

// The generated tables (version_changelog.go, messages_data.go) must equal
// the Python sources they were generated from; regenerate them with
// testdata/gen_changelog.py and testdata/gen_messages.py when this fails.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

const backendAgents = "../../../backend/agents/"

func readBackend(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(backendAgents + name)
	if err != nil {
		t.Fatalf("Python source: %v", err)
	}
	return string(data)
}

func TestVersionTableMatchesPython(t *testing.T) {
	source := readBackend(t, "version.py")
	latest := regexp.MustCompile(`(?m)^LATEST_AGENT_VERSION = "([^"]*)"$`).FindStringSubmatch(source)
	if latest == nil || latest[1] != LatestAgentVersion {
		t.Fatalf("LATEST_AGENT_VERSION %v, Go %s", latest, LatestAgentVersion)
	}
	block := regexp.MustCompile(`(?s)\nVERSION_CHANGELOG = (\{.*?\n\})\n`).FindStringSubmatch(source)
	if block == nil {
		t.Fatal("VERSION_CHANGELOG not found")
	}
	// The dict literal is JSON apart from trailing commas.
	text := regexp.MustCompile(`,(\s*[\]}])`).ReplaceAllString(block[1], "$1")
	python, err := jsonx.ParseString(text)
	if err != nil {
		t.Fatalf("VERSION_CHANGELOG is not JSON-like: %v", err)
	}
	if got, want := compact(t, ChangelogObject()), compact(t, python); got != want {
		t.Fatal("version_changelog.go differs from backend/agents/version.py; regenerate it")
	}
}

func pythonTable(t *testing.T, source, name string) []messagePair {
	t.Helper()
	block := regexp.MustCompile(`(?s)\n` + name + ` = \{\n(.*?)\n\}\n`).FindStringSubmatch(source)
	if block == nil {
		t.Fatalf("%s not found", name)
	}
	line := regexp.MustCompile(`^    '([^'\\]*)': '([^'\\]*)',?$`)
	var pairs []messagePair
	for _, text := range strings.Split(block[1], "\n") {
		match := line.FindStringSubmatch(text)
		if match == nil {
			t.Fatalf("%s: unexpected line %q", name, text)
		}
		pairs = append(pairs, messagePair{match[1], match[2]})
	}
	return pairs
}

func TestMessageTablesMatchPython(t *testing.T) {
	deployment := readBackend(t, "deployment_messages.py")
	upgrade := readBackend(t, "upgrade_messages.py")
	for _, c := range []struct {
		source, name string
		goTable      []messagePair
	}{
		{deployment, "_MESSAGES", deploymentMessages},
		{deployment, "_PREFIXES", deploymentPrefixes},
		{upgrade, "_MESSAGES", upgradeMessages},
		{upgrade, "_CAUSE_PREFIXES", upgradeCausePrefixes},
		{upgrade, "_PATH_PREFIXES", upgradePathPrefixes},
		{upgrade, "_SYSTEM_ERRORS", upgradeSystemErrors},
	} {
		python := pythonTable(t, c.source, c.name)
		if len(python) != len(c.goTable) {
			t.Errorf("%s: %d Python entries, %d Go entries; regenerate messages_data.go", c.name, len(python), len(c.goTable))
			continue
		}
		for i := range python {
			if python[i] != c.goTable[i] {
				t.Errorf("%s[%d]: Python %q, Go %q; regenerate messages_data.go", c.name, i, python[i], c.goTable[i])
			}
		}
	}
}
