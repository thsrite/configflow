package upgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const Version = "1.6.0-go"

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
	if kind != "mihomo" && kind != "mosdns" {
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
	if c.String("service_binary") == "" {
		binary, err := exec.LookPath(kind)
		if err != nil {
			return fmt.Errorf("cannot locate %s core binary", kind)
		}
		c.Values["service_binary"] = binary
	}
	if _, err := os.Stat(c.String("service_binary")); err != nil {
		return fmt.Errorf("core binary does not exist")
	}
	return nil
}

type FileChange struct {
	Path string      `json:"path"`
	Data []byte      `json:"data"`
	Mode os.FileMode `json:"mode"`
}
type Snapshot struct {
	Path    string      `json:"path"`
	Existed bool        `json:"existed"`
	Data    []byte      `json:"data,omitempty"`
	Mode    os.FileMode `json:"mode"`
	UID     int         `json:"uid"`
	GID     int         `json:"gid"`
}

func Capture(path string) (Snapshot, error) {
	s := Snapshot{Path: path}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if !info.Mode().IsRegular() {
		return s, fmt.Errorf("refusing nonregular upgrade target: %s", path)
	}
	s.Existed, s.Mode = true, info.Mode().Perm()
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		s.UID, s.GID = int(st.Uid), int(st.Gid)
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

// MigrationPlan only changes Agent settings and ConfigFlow-owned startup files.
// Core configuration and user unit contents are never rewritten.
func MigrationPlan(c *Config, binary, systemRoot string) ([]FileChange, error) {
	if err := c.InferLifecycle(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(binary) || strings.ContainsAny(binary, "\r\n\x00") {
		return nil, fmt.Errorf("invalid binary path")
	}
	gate := "configflow-recover-" + c.String("service_type")
	unit := c.String("service_unit")
	var changes []FileChange
	if c.String("service_manager") == "systemd" {
		data := "[Unit]\nDescription=ConfigFlow interrupted deployment recovery\nAfter=local-fs.target\nBefore=" + unit + ".service\n\n[Service]\nType=oneshot\nExecStart=" + systemdQuote(binary) + " -config " + systemdQuote(c.Path) + " -recover-only\nRemainAfterExit=yes\n"
		changes = append(changes, FileChange{filepath.Join(systemRoot, "etc/systemd/system", gate+".service"), []byte(data), 0644})
		data = "[Unit]\nRequires=" + gate + ".service\nAfter=" + gate + ".service\n"
		changes = append(changes, FileChange{filepath.Join(systemRoot, "etc/systemd/system", unit+".service.d/configflow-recovery.conf"), []byte(data), 0644})
	} else {
		data := "#!/sbin/openrc-run\nname=\"ConfigFlow deployment recovery\"\ndepend() {\n need localmount\n before " + unit + "\n}\nstart() {\n " + shellQuote(binary) + " -config " + shellQuote(c.Path) + " -recover-only\n}\n"
		changes = append(changes, FileChange{filepath.Join(systemRoot, "etc/init.d", gate), []byte(data), 0755})
		path := filepath.Join(systemRoot, "etc/conf.d", unit)
		original, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		begin, end := "# BEGIN CONFIGFLOW RECOVERY "+c.String("service_type"), "# END CONFIGFLOW RECOVERY "+c.String("service_type")
		text := string(original)
		if start := strings.Index(text, begin); start >= 0 {
			finish := strings.Index(text[start:], end)
			if finish < 0 {
				return nil, fmt.Errorf("malformed OpenRC recovery section")
			}
			text = text[:start] + strings.TrimPrefix(text[start+finish+len(end):], "\n")
		}
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		text += begin + "\nrc_need=\"${rc_need:-} " + gate + "\"\n" + end + "\n"
		mode := os.FileMode(0644)
		if stat, err := os.Stat(path); err == nil {
			mode = stat.Mode().Perm()
		}
		changes = append(changes, FileChange{path, []byte(text), mode})
	}
	c.Values["upgrade_schema"] = 1
	b, err := json.MarshalIndent(c.Values, "", "  ")
	if err != nil {
		return nil, err
	}
	changes = append(changes, FileChange{c.Path, b, 0600})
	return changes, nil
}

func ApplyChanges(changes []FileChange) error {
	for _, change := range changes {
		before, err := Capture(change.Path)
		if err != nil {
			return err
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
