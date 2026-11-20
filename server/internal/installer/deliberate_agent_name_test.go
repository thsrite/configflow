package installer

import (
	"strings"
	"testing"
)

// Coordinator decision 6 (2026-11-16): install_script.validate_agent_name
// uses re.match(r'^[a-zA-Z0-9_-]+$'), whose "$" accepts one trailing "\n";
// the Go ValidateAgentName refuses it like any other invalid name. Corpus
// rows with such a name are counted as deliberate differences.

const invalidAgentNameMessage = "Invalid agent name: only alphanumeric characters, hyphens, and underscores are allowed."

// singleTrailingNewline reports whether value is a string ending in exactly
// one "\n" whose rest passes Python's check.
func singleTrailingNewline(value any) bool {
	text, isString := value.(string)
	rest, found := strings.CutSuffix(text, "\n")
	return isString && found && matchesClass(rest, "_-")
}

// deliberateAgentNameFix reports whether err is Go's refusal of an Agent
// name with one trailing "\n" found under one of keys in object.
func deliberateAgentNameFix(object map[string]any, err error, keys ...string) bool {
	ve, isValue := err.(*ValueError)
	if !isValue || ve.Message != invalidAgentNameMessage {
		return false
	}
	if len(keys) == 0 {
		keys = []string{"agent_name"}
	}
	for _, key := range keys {
		if singleTrailingNewline(object[key]) {
			return true
		}
	}
	return false
}

func TestAgentNameTrailingNewlineIsRefused(t *testing.T) {
	for _, name := range []string{"agent\n", "a-b_C9\n"} {
		if err := ValidateAgentName(name); err == nil || err.Error() != invalidAgentNameMessage {
			t.Errorf("%q: %v", name, err)
		}
		o := DefaultDockerOptions("http://panel.example")
		o.AgentName = name
		if _, err := RenderDockerCompose(o); err == nil || err.Error() != invalidAgentNameMessage {
			t.Errorf("compose %q: %v", name, err)
		}
		if _, err := RenderDockerRun(o); err == nil || err.Error() != invalidAgentNameMessage {
			t.Errorf("run %q: %v", name, err)
		}
	}
	for _, name := range []string{"agent", "A-b_9"} {
		if err := ValidateAgentName(name); err != nil {
			t.Errorf("%q refused: %v", name, err)
		}
	}
}
