package upgrade

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent/internal/atomicfile"
)

const migrationSchema = 2

func removeFile(path string) error {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return atomicfile.SyncDir(filepath.Dir(path))
}

func restoreSymlink(s Snapshot) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".configflow-link-")
	if err != nil {
		return err
	}
	path := f.Name()
	f.Close()
	defer os.Remove(path)
	if err := os.Remove(path); err != nil {
		return err
	}
	if err := os.Symlink(s.LinkTarget, path); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := os.Lchown(path, s.UID, s.GID); err != nil {
			return err
		}
	}
	if err := os.Rename(path, s.Path); err != nil {
		return err
	}
	return atomicfile.SyncDir(filepath.Dir(s.Path))
}

func recoveryCleanupPlan(c *Config, root string) ([]FileChange, error) {
	gate := "configflow-recover-" + c.String("service_type")
	unit := c.String("service_unit")
	var changes []FileChange
	var gatePath string
	if c.String("service_manager") == "systemd" {
		dir := filepath.Join(root, "etc/systemd/system")
		gatePath = filepath.Join(dir, gate+".service")
		paths := []string{filepath.Join(dir, c.AgentUnit()+".service"), filepath.Join(dir, unit+".service")}
		for _, service := range []string{c.AgentUnit(), unit} {
			dropIns, err := filepath.Glob(filepath.Join(dir, service+".service.d/*.conf"))
			if err != nil {
				return nil, err
			}
			paths = append(paths, dropIns...)
		}
		for _, path := range paths {
			change, err := editStartupFile(path, stripSystemdRecoveryDependencies)
			if err != nil {
				return nil, err
			}
			if change != nil {
				if filepath.Base(path) == "configflow-recovery.conf" && !hasSystemdSettings(change.Data) {
					change.Delete = true
					change.Data = nil
				}
				changes = append(changes, *change)
			}
		}
	} else {
		gatePath = filepath.Join(root, "etc/init.d", gate)
		change, err := editStartupFile(filepath.Join(root, "etc/conf.d", unit), func(data []byte) ([]byte, error) {
			return stripOpenRCRecoverySection(data, c.String("service_type"))
		})
		if err != nil {
			return nil, err
		}
		if change != nil {
			changes = append(changes, *change)
		}
		change, err = editStartupFile(filepath.Join(root, "etc/init.d", c.AgentUnit()), stripOpenRCRecoveryDependencies)
		if err != nil {
			return nil, err
		}
		if change != nil {
			changes = append(changes, *change)
		}
		links, err := filepath.Glob(filepath.Join(root, "etc/runlevels/*", gate))
		if err != nil {
			return nil, err
		}
		for _, path := range links {
			changes = append(changes, FileChange{Path: path, Delete: true, AllowSymlink: true})
		}
	}
	// Remove all references first. If the process is interrupted, no remaining
	// dependency should refer to a gate that has already been removed.
	// The fixed ConfigFlow-owned gate may be masked with a /dev/null link.
	// Unlink the gate itself; never follow or modify its target.
	changes = append(changes, FileChange{Path: gatePath, Delete: true, AllowSymlink: true})
	return changes, nil
}

func editStartupFile(path string, edit func([]byte) ([]byte, error)) (*FileChange, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Vendor units are often linked into /etc. Inspect an unaffected unit
		// without replacing its link or modifying a vendor-owned target.
		original, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		data, err := edit(original)
		if err != nil || bytes.Equal(data, original) {
			return nil, err
		}
		return nil, fmt.Errorf("legacy recovery dependency in linked unit requires cleanup at its source: %s", path)
	}
	snapshot, err := Capture(path)
	if err != nil || !snapshot.Existed {
		return nil, err
	}
	data, err := edit(snapshot.Data)
	if err != nil || bytes.Equal(data, snapshot.Data) {
		return nil, err
	}
	return &FileChange{Path: path, Data: data, Mode: snapshot.Mode}, nil
}

func isRecoveryUnit(value string) bool {
	value = strings.TrimSuffix(strings.Trim(value, `"'`), ".service")
	return value == "configflow-recover-mihomo" || value == "configflow-recover-mosdns" || value == "configflow-recover-oxidns"
}

func withoutRecoveryUnits(fields []string) ([]string, bool) {
	result := make([]string, 0, len(fields))
	removed := false
	for _, field := range fields {
		if isRecoveryUnit(field) {
			removed = true
		} else {
			result = append(result, field)
		}
	}
	return result, removed
}

func stripSystemdRecoveryDependencies(data []byte) ([]byte, error) {
	lines := strings.SplitAfter(string(data), "\n")
	var result strings.Builder
	inUnit := false
	for i := 0; i < len(lines); i++ {
		original := lines[i]
		trimmed := strings.TrimSpace(original)
		if strings.HasPrefix(trimmed, "[") {
			inUnit = trimmed == "[Unit]"
		}
		key, value, found := strings.Cut(trimmed, "=")
		if !inUnit || !found || (strings.TrimSpace(key) != "Requires" && strings.TrimSpace(key) != "After") {
			result.WriteString(original)
			continue
		}
		for strings.HasSuffix(strings.TrimSpace(value), "\\") && i+1 < len(lines) {
			value = strings.TrimSuffix(strings.TrimSpace(value), "\\") + " "
			i++
			original += lines[i]
			value += strings.TrimSpace(lines[i])
		}
		fields, changed := withoutRecoveryUnits(strings.Fields(value))
		if !changed {
			result.WriteString(original)
		} else if len(fields) != 0 {
			prefix, _, _ := strings.Cut(original, "=")
			result.WriteString(prefix + "=" + strings.Join(fields, " "))
			if strings.HasSuffix(original, "\n") {
				result.WriteByte('\n')
			}
		}
	}
	return []byte(result.String()), nil
}

func stripOpenRCRecoverySection(data []byte, kind string) ([]byte, error) {
	begin, end := "# BEGIN CONFIGFLOW RECOVERY "+kind, "# END CONFIGFLOW RECOVERY "+kind
	var result strings.Builder
	inside := false
	for _, line := range strings.SplitAfter(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case begin:
			if inside {
				return nil, fmt.Errorf("malformed OpenRC recovery section")
			}
			inside = true
		case end:
			if !inside {
				return nil, fmt.Errorf("malformed OpenRC recovery section")
			}
			inside = false
		default:
			// The marker identifies ConfigFlow's dependency, not ownership of
			// every setting a user may subsequently add inside the section.
			result.WriteString(stripOpenRCRecoveryAssignment(line, "configflow-recover-"+kind))
		}
	}
	if inside {
		return nil, fmt.Errorf("malformed OpenRC recovery section")
	}
	return []byte(result.String()), nil
}

func stripOpenRCRecoveryDependencies(data []byte) ([]byte, error) {
	var result strings.Builder
	for _, line := range strings.SplitAfter(string(data), "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if !strings.HasPrefix(trimmed, "need ") && !strings.HasPrefix(trimmed, "need\t") {
			result.WriteString(line)
			continue
		}
		args, comment, commented := strings.Cut(strings.TrimSpace(trimmed[4:]), "#")
		fields, changed := withoutRecoveryUnits(strings.Fields(args))
		if !changed {
			result.WriteString(line)
			continue
		}
		indent := line[:len(line)-len(trimmed)]
		if len(fields) != 0 {
			result.WriteString(indent + "need " + strings.Join(fields, " "))
			if commented {
				result.WriteString(" #" + comment)
			}
		} else if commented {
			result.WriteString(indent + "#" + comment)
		}
		if strings.HasSuffix(line, "\n") && (len(fields) != 0 || commented) {
			result.WriteByte('\n')
		}
	}
	return []byte(result.String()), nil
}

func hasSystemdSettings(data []byte) bool {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, ";") && !strings.HasPrefix(line, "[") {
			return true
		}
	}
	return false
}

func stripOpenRCRecoveryAssignment(line, gate string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "rc_need=") {
		return line
	}
	changed := false
	for offset := 0; offset < len(line); {
		at := strings.Index(line[offset:], gate)
		if at < 0 {
			break
		}
		at += offset
		end := at + len(gate)
		beforeOK := at == 0 || strings.ContainsRune(" \t\"'=", rune(line[at-1]))
		afterOK := end == len(line) || strings.ContainsRune(" \t\r\n\"';", rune(line[end]))
		if beforeOK && afterOK {
			line = line[:at] + line[end:]
			changed = true
			offset = at
		} else {
			offset = end
		}
	}
	if changed {
		_, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		value = strings.TrimSpace(strings.Trim(strings.TrimSpace(value), "\"'"))
		if value == "" || value == "${rc_need:-}" {
			return ""
		}
	}
	return line
}
