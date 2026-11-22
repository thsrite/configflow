package svc

import (
	"path/filepath"
	"time"
)

// Spec describes one supervised program. The values carry over the former
// Supervisor program sections (docker image, Agent 1.9.2-go and earlier).
type Spec struct {
	Name string
	// Argv is the default command; cores may be given another configuration
	// path at container start (Setup writes an argv override).
	Argv []string
	// StartSecs is how long the program must stay up to count as started;
	// StartRetries is how many quick exits in a row are retried before the
	// program is FATAL and no longer restarted.
	StartSecs    time.Duration
	StartRetries int
	// StopTimeout is how long a stop waits after SIGTERM before SIGKILL.
	StopTimeout time.Duration
	// Logged programs write stdout and stderr together to LogDir/current
	// through s6-log; the others write to the container output.
	Logged bool
	// After lists programs that start before this one and stop after it.
	After []string
}

// Image paths.
const (
	// Scandir is the s6 scan directory of the image (s6-overlay).
	Scandir = "/run/service"
	// CommandDir holds the s6 tools shipped by s6-overlay.
	CommandDir = "/command"
	// StateDir keeps argv overrides and start-retry bookkeeping. It is
	// cleared at every container start.
	StateDir = "/run/configflow-svc"
	// LogRoot holds one s6-log directory per logged program.
	LogRoot = "/var/log/configflow"
	// SourceDir is the s6-rc source directory compiled by s6-overlay at boot.
	SourceDir = "/etc/s6-overlay/s6-rc.d"
	// BundleDir lists the programs s6-overlay brings up at boot.
	BundleDir = "/etc/s6-overlay/user-bundles.d/user/contents.d"
	// Binary is the Agent executable inside the image.
	Binary = "/usr/local/bin/configflow-agent"
)

// Program names that other packages refer to.
const (
	Keepalived    = "configflow-keepalived"
	CompanionServ = "ha-companion-serve"
	// releaseMain stops keepalived before the Agent and core when the
	// main macvlan HA container stops; releaseCompanion does the same in
	// the host-mode companion. Both are oneshots without a process.
	releaseMain      = "ha-release"
	releaseCompanion = "ha-companion-release"
)

const (
	logMaxBytes = 10 * 1024 * 1024 // 10MB per file, as Supervisor's maxbytes
	logBackups  = 3
)

func agentSpec(core string) Spec {
	return Spec{Name: "agent-" + core,
		Argv:      []string{Binary, "-config=/opt/configflow-agent/config-" + core + ".json"},
		StartSecs: time.Second, StartRetries: 3, StopTimeout: 10 * time.Second, After: []string{core}}
}

func coreSpec(core string, argv ...string) Spec {
	return Spec{Name: core, Argv: append([]string{"/usr/local/bin/" + core}, argv...),
		StartSecs: 3 * time.Second, StartRetries: 3, StopTimeout: 10 * time.Second, Logged: true}
}

// Specs lists every program the image can supervise.
var Specs = map[string]Spec{
	"mihomo":       coreSpec("mihomo", "-d", "/etc/mihomo"),
	"mosdns":       coreSpec("mosdns", "start", "-c", "/etc/mosdns/config.yaml", "-d", "/etc/mosdns"),
	"oxidns":       coreSpec("oxidns", "start", "-c", "/etc/oxidns/config.yaml", "-d", "/etc/oxidns"),
	"agent-mihomo": agentSpec("mihomo"),
	"agent-mosdns": agentSpec("mosdns"),
	"agent-oxidns": agentSpec("oxidns"),
	Keepalived: {Name: Keepalived,
		Argv: []string{"/usr/sbin/keepalived", "-P", "--dont-fork", "--log-console",
			"--use-file=/var/lib/configflow-ha/keepalived.conf", "--pid=/run/configflow-ha.pid",
			"--vrrp_pid=/run/configflow-ha-vrrp.pid"},
		StartSecs: time.Second, StartRetries: 3, StopTimeout: 15 * time.Second},
	CompanionServ: {Name: CompanionServ, Argv: []string{Binary, CompanionServ},
		StartSecs: time.Second, StartRetries: 3, StopTimeout: 5 * time.Second},
}

// CoreArgv is the core command for a configuration file: Mihomo takes the
// directory and file, MosDNS and OxiDNS start with -c and the directory.
func CoreArgv(core, config string) []string {
	dir := filepath.Dir(config)
	if core == "mihomo" {
		return []string{"/usr/local/bin/mihomo", "-d", dir, "-f", config}
	}
	return []string{"/usr/local/bin/" + core, "start", "-c", config, "-d", dir}
}

// LogDir is the s6-log directory of a logged program, or "" when the program
// writes to the container output.
func LogDir(name string) string {
	if spec, ok := Specs[name]; ok && spec.Logged {
		return filepath.Join(LogRoot, name)
	}
	return ""
}

// LogFile is the file a logged program is currently writing.
func LogFile(name string) string {
	if dir := LogDir(name); dir != "" {
		return filepath.Join(dir, "current")
	}
	return ""
}
