package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeUnit struct{ stdout, stderr string }

func stubLogEnvironment(t *testing.T, init string, units map[string]fakeUnit) string {
	t.Helper()
	dir := t.TempDir()
	prevInit, prevSystemd, prevOpenRC, prevConf, prevMain, prevLegacy, prevJournal :=
		logDetectInitSystem, logSystemdOutputs, logOpenRCInitDir, logSupervisorConfDir, logSupervisorMainConf, logLegacyFallbacks, logReadJournal
	t.Cleanup(func() {
		logDetectInitSystem, logSystemdOutputs, logOpenRCInitDir, logSupervisorConfDir, logSupervisorMainConf, logLegacyFallbacks, logReadJournal =
			prevInit, prevSystemd, prevOpenRC, prevConf, prevMain, prevLegacy, prevJournal
	})
	logDetectInitSystem = func() string { return init }
	logSystemdOutputs = func(unit string) (string, string, bool) {
		u, ok := units[unit]
		return u.stdout, u.stderr, ok
	}
	logOpenRCInitDir = filepath.Join(dir, "init.d")
	logSupervisorConfDir = filepath.Join(dir, "conf.d")
	logSupervisorMainConf = filepath.Join(dir, "supervisord.conf")
	logLegacyFallbacks = func(*Config) []string { return nil }
	logReadJournal = func(unit string, lines int) (string, error) { return "journal of " + unit + "\n", nil }
	os.MkdirAll(logOpenRCInitDir, 0o755)
	os.MkdirAll(logSupervisorConfDir, 0o755)
	return dir
}

func sourceByID(t *testing.T, sources []LogSource, id string) LogSource {
	t.Helper()
	for _, source := range sources {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("source %q missing from %+v", id, sources)
	return LogSource{}
}

func TestLogSourcesSystemdJournal(t *testing.T) {
	dir := stubLogEnvironment(t, "systemd", map[string]fakeUnit{
		"configflow-agent": {"journal", "inherit"},
		"oxidns":           {"journal", "inherit"},
	})
	cfg := &Config{ServiceType: "oxidns", ServiceManager: "systemd", ServiceUnit: "oxidns", ConfigPath: filepath.Join(dir, "config.yaml")}
	sources := ResolveLogSources(cfg)
	agent, core := sourceByID(t, sources, "agent"), sourceByID(t, sources, "core")
	if agent.Kind != "journal" || agent.Unit != "configflow-agent" || agent.Clearable {
		t.Fatalf("agent source = %+v", agent)
	}
	if core.Kind != "journal" || core.Unit != "oxidns" {
		t.Fatalf("core source = %+v", core)
	}
	logs, err := ReadLogSource(core, 50)
	if err != nil || logs != "journal of oxidns\n" {
		t.Fatalf("logs=%q err=%v", logs, err)
	}
}

func TestLogSourcesSystemdAppendFile(t *testing.T) {
	dir := stubLogEnvironment(t, "systemd", map[string]fakeUnit{})
	agentLog := filepath.Join(dir, "configflow-agent-mihomo.log")
	os.WriteFile(agentLog, []byte("agent line\n"), 0o644)
	logSystemdOutputs = func(unit string) (string, string, bool) {
		if unit == "configflow-agent-mihomo" {
			return "append:" + agentLog, "append:" + agentLog, true
		}
		return "", "", false
	}
	cfg := &Config{ServiceType: "mihomo", ServiceManager: "systemd", ServiceUnit: "mihomo", AgentServiceUnit: "configflow-agent-mihomo.service", ConfigPath: filepath.Join(dir, "config.yaml")}
	sources := ResolveLogSources(cfg)
	agent := sourceByID(t, sources, "agent")
	if agent.Kind != "file" || agent.Path != agentLog || !agent.Clearable {
		t.Fatalf("agent source = %+v", agent)
	}
	for _, source := range sources {
		if source.ID == "agent-err" {
			t.Fatalf("identical stdout/stderr must not be listed twice: %+v", sources)
		}
	}
	if core := sourceByID(t, sources, "core"); core.Kind != "unavailable" || core.Note == "" {
		t.Fatalf("unknown core unit must explain itself: %+v", core)
	}
}

func TestLogSourcesOpenRCOutputLog(t *testing.T) {
	dir := stubLogEnvironment(t, "openrc", nil)
	coreLog := filepath.Join(dir, "mihomo.log")
	os.WriteFile(coreLog, []byte("core\n"), 0o644)
	os.WriteFile(filepath.Join(logOpenRCInitDir, "mihomo"), []byte("#!/sbin/openrc-run\noutput_log=\""+coreLog+"\"\nerror_log=\""+coreLog+"\"\n"), 0o755)
	os.WriteFile(filepath.Join(logOpenRCInitDir, "configflow-agent"), []byte("output_log=\""+filepath.Join(dir, "missing.log")+"\"\n"), 0o755)
	cfg := &Config{ServiceType: "mihomo", ServiceManager: "openrc", ServiceUnit: "mihomo", ConfigPath: filepath.Join(dir, "config.yaml")}
	sources := ResolveLogSources(cfg)
	if core := sourceByID(t, sources, "core"); core.Kind != "file" || core.Path != coreLog {
		t.Fatalf("core source = %+v", core)
	}
	agent := sourceByID(t, sources, "agent")
	if agent.Kind != "unavailable" || agent.Path != filepath.Join(dir, "missing.log") {
		t.Fatalf("missing agent log must keep its configured path: %+v", agent)
	}
}

func TestLogSourcesSupervisorDocker(t *testing.T) {
	dir := stubLogEnvironment(t, "supervisor", nil)
	out, errOut := filepath.Join(dir, "mihomo.log"), filepath.Join(dir, "mihomo.err.log")
	os.WriteFile(out, []byte("out\n"), 0o644)
	os.WriteFile(errOut, []byte("err\n"), 0o644)
	os.WriteFile(filepath.Join(logSupervisorConfDir, "mihomo.conf"), []byte("[program:mihomo]\n; stdout_logfile=/ignored\nstdout_logfile="+out+"\nstderr_logfile="+errOut+"\n"), 0o644)
	os.WriteFile(filepath.Join(logSupervisorConfDir, "agent-mihomo.conf"), []byte("[program:agent-mihomo]\nstdout_logfile=/dev/stdout\nstderr_logfile=/dev/stderr\n"), 0o644)
	cfg := &Config{ServiceType: "mihomo", ServiceManager: "supervisor", ServiceUnit: "mihomo", ConfigPath: filepath.Join(dir, "config.yaml")}
	sources := ResolveLogSources(cfg)
	if core := sourceByID(t, sources, "core"); core.Path != out || core.Kind != "file" {
		t.Fatalf("core = %+v", core)
	}
	if coreErr := sourceByID(t, sources, "core-err"); coreErr.Path != errOut {
		t.Fatalf("core-err = %+v", coreErr)
	}
	if agent := sourceByID(t, sources, "agent"); agent.Kind != "unavailable" || !strings.Contains(agent.Note, "docker logs") {
		t.Fatalf("docker agent = %+v", agent)
	}
}

func TestLogSourcesOxiDNSConfiguredFileAndFallback(t *testing.T) {
	dir := stubLogEnvironment(t, "openrc", nil)
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte("log:\n  file: logs/oxidns.log\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	os.WriteFile(filepath.Join(dir, "logs", "oxidns.log"), []byte("x\n"), 0o644)
	legacy := filepath.Join(dir, "legacy.log")
	os.WriteFile(legacy, []byte("legacy\n"), 0o644)
	logLegacyFallbacks = func(*Config) []string { return []string{filepath.Join(dir, "nope.log"), legacy} }
	cfg := &Config{ServiceType: "oxidns", ServiceManager: "openrc", ServiceUnit: "oxidns", ConfigPath: configPath}
	sources := ResolveLogSources(cfg)
	if file := sourceByID(t, sources, "core-file"); file.Path != filepath.Join(dir, "logs", "oxidns.log") || file.Kind != "file" {
		t.Fatalf("core-file = %+v", file)
	}
	if core := sourceByID(t, sources, "core"); core.Path != legacy {
		t.Fatalf("legacy fallback = %+v", core)
	}
}

func TestReadLogSourceStripsANSIAndReportsMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.log")
	os.WriteFile(path, []byte("\x1b[3mtag\x1b[0m\x1b[2m=\x1b[0mvalue\n"), 0o644)
	logs, err := ReadLogSource(LogSource{Kind: "file", Path: path}, 10)
	if err != nil || logs != "tag=value\n" {
		t.Fatalf("logs=%q err=%v", logs, err)
	}
	if _, err := ReadLogSource(LogSource{Kind: "file", Path: filepath.Join(dir, "missing.log")}, 10); err == nil || !strings.Contains(err.Error(), "文件不存在") {
		t.Fatalf("missing file err = %v", err)
	}
}

func TestClearLogRejectsJournalAndNonLogFiles(t *testing.T) {
	dir := stubLogEnvironment(t, "systemd", map[string]fakeUnit{"configflow-agent": {"journal", "journal"}})
	cfg := &Config{ServiceType: "oxidns", ServiceManager: "systemd", ServiceUnit: "oxidns", ConfigPath: filepath.Join(dir, "config.yaml")}
	config := filepath.Join(dir, "config.yaml")
	os.WriteFile(config, []byte("keep"), 0o644)
	clear := func(body map[string]string) map[string]interface{} {
		data, _ := json.Marshal(body)
		rec := httptest.NewRecorder()
		ClearLogHandler(cfg)(rec, httptest.NewRequest(http.MethodPost, "/api/logs/clear", bytes.NewReader(data)))
		var result map[string]interface{}
		json.Unmarshal(rec.Body.Bytes(), &result)
		return result
	}
	if result := clear(map[string]string{"source": "agent"}); result["success"] != false {
		t.Fatalf("journal source cleared: %v", result)
	}
	if result := clear(map[string]string{"log_path": config}); result["success"] != false {
		t.Fatalf("non-log file cleared: %v", result)
	}
	if data, _ := os.ReadFile(config); string(data) != "keep" {
		t.Fatalf("config truncated")
	}
}

func TestGetLogsBySourceReturnsReadableError(t *testing.T) {
	dir := stubLogEnvironment(t, "openrc", nil)
	os.WriteFile(filepath.Join(logOpenRCInitDir, "oxidns"), []byte("output_log="+filepath.Join(dir, "oxidns.log")+"\n"), 0o755)
	cfg := &Config{ServiceType: "oxidns", ServiceManager: "openrc", ServiceUnit: "oxidns", ConfigPath: filepath.Join(dir, "config.yaml")}
	rec := httptest.NewRecorder()
	GetLogsHandler(cfg)(rec, httptest.NewRequest(http.MethodGet, "/api/logs?source=core", nil))
	var result map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &result)
	if result["success"] != false || !strings.Contains(result["message"].(string), "尚未生成") {
		t.Fatalf("result = %v", result)
	}
}

func TestOpenRCCoreLogPaths(t *testing.T) {
	dir := stubLogEnvironment(t, "openrc", nil)
	out, errOut := filepath.Join(dir, "mihomo.log"), filepath.Join(dir, "mihomo.err.log")
	write := func(script string) {
		if err := os.WriteFile(filepath.Join(logOpenRCInitDir, "mihomo"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &Config{ServiceType: "mihomo", ServiceManager: "openrc", ServiceUnit: "mihomo.service"}
	write("#!/sbin/openrc-run\noutput_log=\"" + out + "\"\nerror_log=\"" + out + "\"\n")
	if got := OpenRCCoreLogPaths(cfg); len(got) != 1 || got[0] != out {
		t.Fatalf("shared output = %v", got)
	}
	write("#!/sbin/openrc-run\noutput_log=\"" + out + "\"\nerror_log=\"" + errOut + "\"\n")
	if got := OpenRCCoreLogPaths(cfg); len(got) != 2 || got[0] != out || got[1] != errOut {
		t.Fatalf("separate outputs = %v", got)
	}
	// Devices and relative paths are never compacted.
	write("#!/sbin/openrc-run\noutput_log=\"/dev/null\"\nerror_log=\"relative.log\"\n")
	if got := OpenRCCoreLogPaths(cfg); len(got) != 0 {
		t.Fatalf("unusable outputs = %v", got)
	}
	cfg.ServiceManager = "systemd"
	if got := OpenRCCoreLogPaths(cfg); got != nil {
		t.Fatalf("systemd core = %v", got)
	}
}
