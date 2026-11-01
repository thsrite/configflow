package upgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const Version = "1.8.51-go"

type Config struct {
	Path   string
	Values map[string]interface{}
}

func ReadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c := &Config{Path: path}
	if err = json.Unmarshal(b, &c.Values); err != nil {
		return nil, err
	}
	if c.Values == nil {
		return nil, fmt.Errorf("empty Agent configuration")
	}
	return c, nil
}
func (c *Config) String(key string) string { value, _ := c.Values[key].(string); return value }
func (c *Config) Port() int                { value, _ := c.Values["agent_port"].(float64); return int(value) }
func (c *Config) Docker() bool {
	return c.String("deployment_method") == "docker" || c.String("service_manager") == "supervisor" || strings.Contains(c.String("restart_command"), "supervisorctl")
}
func (c *Config) AgentUnit() string {
	if value := c.String("agent_service_unit"); value != "" {
		return strings.TrimSuffix(value, ".service")
	}
	return "configflow-agent"
}

var unitPattern = regexp.MustCompile(`^[A-Za-z0-9_@.-]+$`)
var standardRestart = regexp.MustCompile(`^(?:/(?:usr/)?(?:s?bin)/)?(systemctl|rc-service) (?:restart ([A-Za-z0-9_@.-]+)|([A-Za-z0-9_@.-]+) restart)$`)

func (c *Config) InferLifecycle() error {
	if c.Docker() {
		return fmt.Errorf("Docker Agent requires an image update; container binary replacement is unsupported")
	}
	kind := c.String("service_type")
	if kind != "mihomo" && kind != "mosdns" && kind != "oxidns" {
		return fmt.Errorf("unsupported core service")
	}
	manager, unit := c.String("service_manager"), c.String("service_unit")
	if manager == "" {
		parts := standardRestart.FindStringSubmatch(strings.TrimSpace(c.String("restart_command")))
		if parts == nil {
			return fmt.Errorf("cannot infer custom lifecycle; configure service_manager and service_unit before upgrading")
		}
		manager = "systemd"
		if parts[1] == "rc-service" {
			manager = "openrc"
		}
		unit = parts[2] + parts[3]
	}
	if manager != "systemd" && manager != "openrc" {
		return fmt.Errorf("online migration supports systemd and OpenRC")
	}
	unit = strings.TrimSuffix(unit, ".service")
	if !unitPattern.MatchString(unit) || !unitPattern.MatchString(c.AgentUnit()) {
		return fmt.Errorf("invalid service unit")
	}
	if !filepath.IsAbs(c.Path) || !filepath.IsAbs(c.String("config_path")) {
		return fmt.Errorf("Agent and core config paths must be absolute")
	}
	if strings.ContainsAny(c.Path, "\r\n\x00") {
		return fmt.Errorf("invalid Agent config path")
	}
	if c.Port() < 1 || c.Port() > 65535 {
		return fmt.Errorf("invalid Agent API port")
	}
	c.Values["service_manager"], c.Values["service_unit"] = manager, unit
	// Agent migration only updates lifecycle metadata. The core may be
	// installed later, and configured command names are resolved through PATH
	// when deployment invokes the core, not relative to the Agent directory.
	if c.String("service_binary") == "" {
		c.Values["service_binary"] = kind
	}
	return nil
}

type FileChange struct {
	Path         string      `json:"path"`
	Data         []byte      `json:"data"`
	Mode         os.FileMode `json:"mode"`
	Delete       bool        `json:"delete,omitempty"`
	AllowSymlink bool        `json:"allow_symlink,omitempty"`
}
type Snapshot struct {
	Path       string      `json:"path"`
	Existed    bool        `json:"existed"`
	Data       []byte      `json:"data,omitempty"`
	Mode       os.FileMode `json:"mode"`
	UID        int         `json:"uid"`
	GID        int         `json:"gid"`
	LinkTarget string      `json:"link_target,omitempty"`
}

func Capture(path string) (Snapshot, error) {
	return capture(path, false)
}

func captureChange(change FileChange) (Snapshot, error) {
	return capture(change.Path, change.Delete && change.AllowSymlink)
}

func capture(path string, allowSymlink bool) (Snapshot, error) {
	s := Snapshot{Path: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() && !(allowSymlink && info.Mode()&os.ModeSymlink != 0) {
		return s, fmt.Errorf("refusing nonregular upgrade target: %s", path)
	}
	s.Existed, s.Mode = true, info.Mode().Perm()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		s.UID, s.GID = int(st.Uid), int(st.Gid)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		s.LinkTarget, err = os.Readlink(path)
		return s, err
	}
	s.Data, err = os.ReadFile(path)
	return s, err
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".configflow-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(mode); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func Restore(snapshots []Snapshot) error {
	for i := len(snapshots) - 1; i >= 0; i-- {
		s := snapshots[i]
		if !s.Existed {
			if err := os.Remove(s.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if s.LinkTarget != "" {
			if err := restoreSymlink(s); err != nil {
				return err
			}
			continue
		}
		if err := AtomicWrite(s.Path, s.Data, s.Mode); err != nil {
			return err
		}
		if os.Geteuid() == 0 {
			if err := os.Chown(s.Path, s.UID, s.GID); err != nil {
				return err
			}
		}
	}
	return nil
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
func systemdQuote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(value) + `"`
}

// MigrationPlan removes ConfigFlow's old mandatory recovery gates. Core
// configuration and unrelated settings in service definitions are preserved.
func MigrationPlan(c *Config, binary, systemRoot string) ([]FileChange, error) {
	if err := c.InferLifecycle(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(binary) || strings.ContainsAny(binary, "\r\n\x00") {
		return nil, fmt.Errorf("invalid binary path")
	}
	changes, err := recoveryCleanupPlan(c, systemRoot)
	if err != nil {
		return nil, err
	}
	c.Values["upgrade_schema"] = migrationSchema
	b, err := json.MarshalIndent(c.Values, "", "  ")
	if err != nil {
		return nil, err
	}
	changes = append(changes, FileChange{Path: c.Path, Data: b, Mode: 0600})
	return changes, nil
}

func ApplyChanges(changes []FileChange) error {
	for _, change := range changes {
		before, err := captureChange(change)
		if err != nil {
			return err
		}
		if change.Delete {
			if err := removeFile(change.Path); err != nil {
				return err
			}
			continue
		}
		if err = AtomicWrite(change.Path, change.Data, change.Mode); err != nil {
			return err
		}
		if before.Existed && os.Geteuid() == 0 {
			if err = os.Chown(change.Path, before.UID, before.GID); err != nil {
				return err
			}
		}
	}
	return nil
}
