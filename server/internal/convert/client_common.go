package convert

import (
	"strings"

	"configflow/internal/model"
)

// customSection is an ordered INI section, including its original header.
type customSection struct{ name, text string }

func splitClientSections(custom string) []customSection {
	var sections []customSection
	index := map[string]int{}
	store := func(name string, lines []string) {
		text := strings.Join(lines, "\n")
		if i, ok := index[name]; ok {
			sections[i].text = text
			return
		}
		index[name] = len(sections)
		sections = append(sections, customSection{name, text})
	}
	current := ""
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(custom), "\n") {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "[") && strings.Contains(stripped, "]") {
			if current != "" {
				store(current, lines)
			}
			before, _, _ := strings.Cut(stripped, "]")
			current = strings.Split(before, "[")[1]
			lines = []string{line}
		} else if current != "" {
			lines = append(lines, line)
		}
	}
	if current != "" {
		store(current, lines)
	}
	return sections
}
func clientSection(sections []customSection, name string) (string, bool) {
	for _, section := range sections {
		if section.name == name {
			return section.text, true
		}
	}
	return "", false
}
func clientAPIPath(config *model.Config, target string) string {
	if config != nil && config.ProfileID != "" {
		return "/api/config/" + config.ProfileID + "/" + target
	}
	return "/api/config/" + target
}
func clientBaseURL(config *model.Config, fallback string) string {
	if config != nil {
		if domain := strings.TrimSpace(config.SystemConfig.ServerDomain); domain != "" {
			return domain
		}
	}
	return fallback
}
func clientPolicyNames(lines []string) []string {
	var names []string
	for _, line := range lines {
		if before, _, ok := strings.Cut(line, "="); ok {
			names = append(names, strings.TrimSpace(before))
		}
	}
	return names
}
