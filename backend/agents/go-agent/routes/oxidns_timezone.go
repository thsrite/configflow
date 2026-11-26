package routes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OxiDNS resolves IANA names through jiff, which on Linux reads only the
// system zoneinfo database (these directories, in this order). Minimal Alpine
// installs ship without tzdata, so the core would exit while starting.
var zoneinfoDirectories = []string{"/usr/share/zoneinfo", "/usr/share/lib/zoneinfo", "/etc/zoneinfo"}

// oxidnsPluginTimezone returns the timezone a plugin resolves from the
// database at startup, or "" when it needs none.
func oxidnsPluginTimezone(kind string, args map[string]any) string {
	zone, _ := args["timezone"].(string)
	zone = strings.TrimSpace(zone)
	switch kind {
	case "cron":
		// Interval jobs never parse the timezone; only cron schedules do.
		jobs, _ := args["jobs"].([]any)
		scheduled := false
		for _, value := range jobs {
			job, _ := value.(map[string]any)
			if schedule, _ := job["schedule"].(string); strings.TrimSpace(schedule) != "" {
				scheduled = true
			}
		}
		if !scheduled {
			return ""
		}
	case "time":
	default:
		return ""
	}
	// UTC is built in and cron also accepts fixed offsets such as +08:00. An
	// omitted timezone falls back to the system zone, or UTC without tzdata.
	if zone == "" || strings.EqualFold(zone, "UTC") || strings.HasPrefix(zone, "+") || strings.HasPrefix(zone, "-") {
		return ""
	}
	return zone
}

// zoneinfoAvailable reports whether the first readable zoneinfo directory
// contains name. Names are matched case-insensitively like jiff does.
func zoneinfoAvailable(name string) bool {
	for _, dir := range zoneinfoDirectories {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		current := dir
		for _, part := range strings.Split(name, "/") {
			entries, err := os.ReadDir(current)
			if err != nil || part == "" || part == "." || part == ".." {
				return false
			}
			found := ""
			for _, entry := range entries {
				if strings.EqualFold(entry.Name(), part) {
					found = entry.Name()
					break
				}
			}
			if found == "" {
				return false
			}
			current = filepath.Join(current, found)
		}
		info, err := os.Stat(current)
		return err == nil && info.Mode().IsRegular()
	}
	return false
}

func validateOxiDNSTimezone(kind string, args map[string]any) error {
	zone := oxidnsPluginTimezone(kind, args)
	if zone == "" || zoneinfoAvailable(zone) {
		return nil
	}
	return fmt.Errorf("timezone data for %s is missing on this device; install tzdata (Alpine: apk add tzdata) and publish again", zone)
}
