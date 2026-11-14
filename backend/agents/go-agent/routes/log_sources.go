package routes

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LogSource is one readable log of this installation. Paths are resolved from
// the actual service definitions on the machine, never from install defaults.
type LogSource struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Kind      string `json:"kind"` // file | journal | unavailable
	Path      string `json:"path,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Clearable bool   `json:"clearable"`
	Note      string `json:"note,omitempty"`
}

const maxLogLines = 10000

var (
	logOpenRCInitDir      = "/etc/init.d"
	logSupervisorConfDir  = "/etc/supervisor/conf.d"
	logSupervisorMainConf = "/etc/supervisor/supervisord.conf"
	logLegacyFallbacks    = legacyCoreLogPaths
	logDetectInitSystem   = detectInitSystem
	logSystemdOutputs     = systemdUnitOutputs
	logReadJournal        = readJournal
)

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func coreDisplayName(serviceType string) string {
	switch serviceType {
	case "mihomo":
		return "Mihomo"
	case "oxidns":
		return "OxiDNS"
	case "mosdns":
		return "MosDNS"
	}
	return serviceType
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// systemdUnitOutputs returns the StandardOutput/StandardError targets of a
// unit, or ok=false when the unit is not installed.
func systemdUnitOutputs(unit string) (stdout, stderr string, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "show", unit, "--property=LoadState", "--property=StandardOutput", "--property=StandardError").Output()
	if err != nil {
		return "", "", false
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if key, value, found := strings.Cut(strings.TrimSpace(line), "="); found {
			values[key] = value
		}
	}
	if values["LoadState"] != "loaded" {
		return "", "", false
	}
	return values["StandardOutput"], values["StandardError"], true
}

// systemdOutputFile extracts the path of file:, append: and truncate: targets.
func systemdOutputFile(target string) string {
	for _, prefix := range []string{"append:", "file:", "truncate:"} {
		if strings.HasPrefix(target, prefix) {
			return strings.TrimPrefix(target, prefix)
		}
	}
	return ""
}

func readJournal(unit string, lines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(lines), "--no-pager", "-o", "cat").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("journalctl 读取失败: %s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// openrcOutputs reads output_log/error_log from an OpenRC service script.
func openrcOutputs(unit string) (stdout, stderr string) {
	file, err := os.Open(filepath.Join(logOpenRCInitDir, unit))
	if err != nil {
		return "", ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !found {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		switch key {
		case "output_log":
			stdout = value
		case "error_log":
			stderr = value
		}
	}
	return stdout, stderr
}

// OpenRCCoreLogPaths lists the files an OpenRC-managed core appends its output
// to. OpenRC never rotates them, unlike the systemd journal and Supervisor.
func OpenRCCoreLogPaths(cfg *Config) []string {
	if coreServiceManager(cfg) != "openrc" {
		return nil
	}
	unit := trimUnit(cfg.ServiceUnit)
	if unit == "" {
		unit = cfg.ServiceType
	}
	stdout, stderr := openrcOutputs(unit)
	var paths []string
	for _, path := range []string{stdout, stderr} {
		if usableOutputFile(path) && filepath.IsAbs(path) && (len(paths) == 0 || paths[0] != path) {
			paths = append(paths, path)
		}
	}
	return paths
}

// supervisorOutputs reads stdout_logfile/stderr_logfile of [program:name].
func supervisorOutputs(program string) (stdout, stderr string) {
	files, _ := filepath.Glob(filepath.Join(logSupervisorConfDir, "*.conf"))
	files = append(files, logSupervisorMainConf)
	header := "[program:" + program + "]"
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		inProgram := false
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "[") {
				inProgram = line == header
				continue
			}
			if !inProgram || strings.HasPrefix(line, ";") {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if !found {
				continue
			}
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "stdout_logfile":
				stdout = value
			case "stderr_logfile":
				stderr = value
			}
		}
		if stdout != "" || stderr != "" {
			return stdout, stderr
		}
	}
	return "", ""
}

// configuredCoreLogFile reads log.file from OxiDNS/MosDNS configuration.
func configuredCoreLogFile(cfg *Config) string {
	if cfg.ServiceType != "oxidns" && cfg.ServiceType != "mosdns" {
		return ""
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil {
		return ""
	}
	var doc struct {
		Log struct {
			File string `yaml:"file"`
		} `yaml:"log"`
	}
	if yaml.Unmarshal(data, &doc) != nil || doc.Log.File == "" {
		return ""
	}
	path := doc.Log.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(cfg.ConfigPath), path)
	}
	return path
}

func legacyCoreLogPaths(cfg *Config) []string {
	t := cfg.ServiceType
	configDir := filepath.Dir(cfg.ConfigPath)
	paths := []string{
		"/var/log/supervisor/" + t + ".log",
		"/var/log/" + t + ".log",
		filepath.Join(configDir, t+".log"),
		"/etc/" + t + "/" + t + ".log",
	}
	if t == "mosdns" {
		paths = append(paths, filepath.Join(configDir, "mosdns.err.log"))
	}
	return paths
}

func usableOutputFile(path string) bool {
	return path != "" && !strings.HasPrefix(path, "/dev/") && !strings.EqualFold(path, "AUTO") && !strings.EqualFold(path, "NONE")
}

// serviceLogSources resolves stdout/stderr of one service from its manager.
// Missing files are kept as unavailable sources so the UI can say where the
// service would write instead of showing an empty pane.
func serviceLogSources(manager, unit, idPrefix, label string) []LogSource {
	var stdout, stderr string
	switch manager {
	case "systemd":
		out, errOut, ok := logSystemdOutputs(unit)
		if !ok {
			return nil
		}
		stdout, stderr = systemdOutputFile(out), systemdOutputFile(errOut)
		if stdout == "" && stderr == "" {
			return []LogSource{{ID: idPrefix, Label: label + "（系统日志）", Kind: "journal", Unit: unit}}
		}
	case "openrc":
		stdout, stderr = openrcOutputs(unit)
	case "supervisor":
		stdout, stderr = supervisorOutputs(unit)
		if strings.HasPrefix(stdout, "/dev/") && (stderr == "" || strings.HasPrefix(stderr, "/dev/")) {
			return []LogSource{{ID: idPrefix, Label: label, Kind: "unavailable", Note: "输出直接写入容器标准输出，请在宿主机用 docker logs 查看"}}
		}
	default:
		return nil
	}
	var sources []LogSource
	if usableOutputFile(stdout) {
		sources = append(sources, fileLogSource(idPrefix, label, stdout))
	}
	if usableOutputFile(stderr) && stderr != stdout {
		sources = append(sources, fileLogSource(idPrefix+"-err", label+"（错误输出）", stderr))
	}
	return sources
}

func fileLogSource(id, label, path string) LogSource {
	if regularFileExists(path) {
		return LogSource{ID: id, Label: label, Kind: "file", Path: path, Clearable: true}
	}
	return LogSource{ID: id, Label: label, Kind: "unavailable", Path: path, Note: "服务配置指向该文件，但文件尚未生成"}
}

func trimUnit(unit string) string {
	return strings.TrimSuffix(strings.TrimSpace(unit), ".service")
}

func coreServiceManager(cfg *Config) string {
	switch cfg.ServiceManager {
	case "systemd", "openrc", "supervisor":
		return cfg.ServiceManager
	}
	return logDetectInitSystem()
}

func hasReadableLogSource(sources []LogSource) bool {
	for _, source := range sources {
		if source.Kind != "unavailable" {
			return true
		}
	}
	return false
}

func agentLogSources(cfg *Config) []LogSource {
	manager := coreServiceManager(cfg)
	unit := trimUnit(cfg.AgentServiceUnit)
	if unit == "" {
		unit = "configflow-agent"
		if manager == "supervisor" {
			unit = "agent-" + cfg.ServiceType
		}
	}
	sources := serviceLogSources(manager, unit, "agent", "Agent 日志")
	if hasReadableLogSource(sources) {
		return sources
	}
	// Older shell installs append to a fixed file outside the unit definition.
	if path := AgentLogPath(cfg); path != "" && regularFileExists(path) {
		return []LogSource{{ID: "agent", Label: "Agent 日志", Kind: "file", Path: path, Clearable: true}}
	}
	if len(sources) > 0 {
		return sources
	}
	if logDetectInitSystem() == "systemd" {
		return []LogSource{{ID: "agent", Label: "Agent 日志（系统日志）", Kind: "journal", Unit: unit}}
	}
	return []LogSource{{ID: "agent", Label: "Agent 日志", Kind: "unavailable", Note: "未找到 Agent 的日志输出位置，可用自定义路径查看"}}
}

func coreLogSources(cfg *Config) []LogSource {
	if cfg.ServiceType == "" {
		return nil
	}
	name := coreDisplayName(cfg.ServiceType)
	var sources []LogSource
	if path := configuredCoreLogFile(cfg); path != "" {
		sources = append(sources, fileLogSource("core-file", name+" 日志文件", path))
	}
	unit := trimUnit(cfg.ServiceUnit)
	if unit == "" {
		unit = cfg.ServiceType
	}
	managed := serviceLogSources(coreServiceManager(cfg), unit, "core", name+" 运行日志")
	for _, source := range managed {
		if source.Path != "" && len(sources) > 0 && sources[0].Path == source.Path {
			continue
		}
		sources = append(sources, source)
	}
	if len(managed) > 0 {
		return sources
	}
	for _, path := range logLegacyFallbacks(cfg) {
		if regularFileExists(path) {
			return append(sources, LogSource{ID: "core", Label: name + " 运行日志", Kind: "file", Path: path, Clearable: true})
		}
	}
	if len(sources) > 0 {
		return sources
	}
	return []LogSource{{ID: "core", Label: name + " 运行日志", Kind: "unavailable", Note: "未找到 " + name + " 的日志输出位置，可用自定义路径查看"}}
}

// ResolveLogSources lists the Agent, core and supervisor logs of this host.
func ResolveLogSources(cfg *Config) []LogSource {
	sources := append(agentLogSources(cfg), coreLogSources(cfg)...)
	if coreServiceManager(cfg) == "supervisor" && regularFileExists("/var/log/supervisor/supervisord.log") {
		sources = append(sources, LogSource{ID: "supervisord", Label: "Supervisor 日志", Kind: "file", Path: "/var/log/supervisor/supervisord.log", Clearable: true})
	}
	return sources
}

func findLogSource(cfg *Config, id string) (LogSource, bool) {
	for _, source := range ResolveLogSources(cfg) {
		if source.ID == id {
			return source, true
		}
	}
	return LogSource{}, false
}

func clampLogLines(lines int) int {
	if lines <= 0 {
		return 100
	}
	if lines > maxLogLines {
		return maxLogLines
	}
	return lines
}

// tailLogFile returns the last lines of a regular file and reports why it
// cannot be read instead of returning an empty string.
func tailLogFile(path string, lines int) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("文件不存在: %s", path)
	}
	if err != nil {
		return "", fmt.Errorf("无法读取 %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("不是普通文件: %s", path)
	}
	out, err := exec.Command("tail", "-n", strconv.Itoa(clampLogLines(lines)), path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取 %s 失败: %s", path, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ReadLogSource reads one resolved source.
func ReadLogSource(source LogSource, lines int) (string, error) {
	var logs string
	var err error
	switch source.Kind {
	case "file":
		logs, err = tailLogFile(source.Path, lines)
	case "journal":
		logs, err = logReadJournal(source.Unit, clampLogLines(lines))
	default:
		return "", fmt.Errorf("%s", source.Note)
	}
	if err != nil {
		return "", err
	}
	return ensureValidUTF8(ansiEscapePattern.ReplaceAllString(logs, "")), nil
}
