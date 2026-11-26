package installer

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func l1InitImage(t *testing.T, manager string) string {
	t.Helper()
	dockerfile := "FROM jrei/systemd-debian:12\nENV DEBIAN_FRONTEND=noninteractive\nRUN apt-get update && apt-get install -y --no-install-recommends curl wget python3 ca-certificates coreutils procps tzdata && rm -rf /var/lib/apt/lists/*\n"
	if manager == "openrc" {
		dockerfile = "FROM alpine:3\nRUN apk add --no-cache openrc curl wget python3 coreutils tzdata grep sed gawk procps\nCMD [\"/bin/sh\",\"-c\",\"mkdir -p /run/openrc; touch /run/openrc/softlevel; exec sleep 100000\"]\n"
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Dockerfile"), dockerfile, 0o644)
	digest := sha256.Sum256([]byte(dockerfile))
	image := fmt.Sprintf("configflow-l1-init-%s:%x", manager, digest[:6])
	if out, err := l1Docker(t, "build", "-t", image, dir); err != nil {
		t.Fatalf("build %s: %v: %s", image, err, out)
	}
	return image
}

const l1CandidateAgent = `#!/bin/sh
# L1_CANDIDATE: real init-manager failure injection, isolated from Agent behavior.
for argument do
    case "$argument" in -version|-validate-service|-check-environment|-check-deployments|-require-clean-deployments) exit 0 ;; esac
done
printf '%s\n' "$*" >> /srv/l1/candidate-starts
case "$*" in */mihomo/config.json*) exec sleep 100000 ;; esac
echo 'L1 candidate start fault' >&2
exit 37
`

const l1OldAgent = `#!/bin/sh
for argument do case "$argument" in -version) exit 0 ;; esac; done
printf '%s\n' "$*" >> /srv/l1/old-starts
exec sleep 100000
`

func l1Unit(manager, unit, binary, config string) string {
	if manager == "systemd" {
		return "[Unit]\nDescription=L1 old Agent\n[Service]\nType=simple\nExecStart=" + binary + " -config " + config + "\nRestart=always\nRestartSec=10\n[Install]\nWantedBy=multi-user.target\n"
	}
	return "#!/sbin/openrc-run\nname=\"L1 old Agent\"\ncommand=\"" + binary + "\"\ncommand_args=\"-config " + config + "\"\nsupervisor=\"supervise-daemon\"\nrespawn_delay=10\nrespawn_max=0\npidfile=\"/run/" + unit + ".pid\"\noutput_log=\"/srv/l1/" + unit + ".log\"\nerror_log=\"/srv/l1/" + unit + ".log\"\n"
}

func l1ContainerSnapshot(t *testing.T, name string, paths []string) string {
	t.Helper()
	python := `import hashlib,json,os,stat,sys
result={}
for name in sys.argv[1:]:
 if os.path.lexists(name):
  result[name]={'mode':stat.S_IMODE(os.lstat(name).st_mode),'link':os.readlink(name)} if os.path.islink(name) else {'mode':stat.S_IMODE(os.stat(name).st_mode),'sha256':hashlib.sha256(open(name,'rb').read()).hexdigest()}
 else: result[name]=None
print(json.dumps(result,sort_keys=True))`
	args := append([]string{"exec", name, "python3", "-c", python}, paths...)
	out, err := l1Docker(t, args...)
	if err != nil {
		t.Fatalf("snapshot: %v: %s", err, out)
	}
	return string(out)
}

func TestL1RollbackWithRealInitManagers(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	for _, manager := range []string{"systemd", "openrc"} {
		image := l1InitImage(t, manager)
		for _, scenario := range []string{"single-existing", "single-new", "aio-existing"} {
			t.Run(manager+"/"+scenario, func(t *testing.T) {
				aio, existing := strings.HasPrefix(scenario, "aio"), strings.HasSuffix(scenario, "existing")
				name := "cfl1d3-" + strings.ToLower(rand.Text()[:12])
				args := []string{"run", "-d", "--name", name, "--label", "configflow.l1-installer=go-fix-l1-installer", "--network", "none", "--tmpfs", "/run", "--tmpfs", "/tmp"}
				if manager == "systemd" {
					args = append(args, "--privileged", "--cgroupns=host", "-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw", "--tmpfs", "/run/lock")
				} else {
					// A real PID 1 must reap daemonized OpenRC services. Sleep alone
					// leaves zombies that supervise-daemon cannot finish stopping.
					args = append(args, "--init")
				}
				t.Cleanup(func() {
					if out, err := l1Docker(t, "rm", "-f", name); err != nil && !strings.Contains(string(out), "No such container") {
						t.Errorf("remove owned container: %v: %s", err, out)
					}
				})
				if out, err := l1Docker(t, append(args, image)...); err != nil {
					t.Fatalf("start init container: %v: %s", err, out)
				}
				t.Logf("owned init container %s", name)
				if manager == "systemd" {
					if out, err := l1Docker(t, "exec", name, "sh", "-c", "i=0; until systemctl list-units >/dev/null 2>&1; do i=$((i+1)); [ \"$i\" -lt 100 ] || exit 1; sleep 0.1; done"); err != nil {
						t.Fatalf("systemd did not boot: %v: %s", err, out)
					}
				}
				files := map[string]string{}
				var paths, units []string
				for _, kind := range []string{"mihomo", "oxidns"} {
					coreBinary, coreConfig := "/usr/local/bin/"+kind, "/etc/"+kind+"/config.yaml"
					coreUnit := "/etc/init.d/" + kind
					if manager == "systemd" {
						coreUnit = "/etc/systemd/system/" + kind + ".service"
					}
					files[coreBinary], files[coreConfig] = "#!/bin/sh\nexit 0\n", "# L1 unchanged core config\n"
					files[coreUnit] = l1Unit(manager, kind, "/bin/sleep", "100000")
					paths = append(paths, coreBinary, coreConfig, coreUnit)
				}
				kinds := []string{"oxidns"}
				if aio {
					kinds = []string{"mihomo", "oxidns"}
				}
				for _, kind := range kinds {
					unit, binary, config := "configflow-agent", "/usr/local/bin/configflow-agent", "/opt/configflow-agent/config-oxidns.json"
					if aio {
						unit, binary, config = "configflow-agent-"+kind, "/opt/configflow-agent/"+kind+"/configflow-agent", "/opt/configflow-agent/"+kind+"/config.json"
					}
					unitPath := "/etc/init.d/" + unit
					if manager == "systemd" {
						unitPath = "/etc/systemd/system/" + unit + ".service"
					}
					units = append(units, unit)
					paths = append(paths, binary, config, unitPath)
					if existing {
						files[binary], files[unitPath] = l1OldAgent, l1Unit(manager, unit, binary, config)
						files[config] = fmt.Sprintf("{\"agent_name\":\"old-%s\",\"agent_id\":\"old-%s-id\",\"token\":\"old-%s-token\",\"service_type\":\"%s\",\"service_unit\":\"%s\",\"service_manager\":\"%s\",\"agent_service_unit\":\"%s\",\"native_aio\":%t,\"config_path\":\"/etc/%s/config.yaml\"}\n", kind, kind, kind, kind, kind, manager, unit, aio, kind)
					}
				}
				const server = "http://127.0.0.1:18751"
				var source string
				var err error
				if aio {
					o := DefaultAIOOptions(server, "l1-replacement")
					o.BinaryDownloadURL, o.ManageIPv4Forwarding, o.ManageTun = server, false, false
					source, err = RenderAIOInstall(o)
				} else {
					source, err = RenderAgentInstall(AgentInstallOptions{AgentPort: 8080, ServerURL: server, AgentName: "l1-replacement", ServiceType: "oxidns", BinaryDownloadURL: server, ManageIPv4Forwarding: ForwardingOff})
				}
				if err != nil {
					t.Fatal(err)
				}
				source = strings.ReplaceAll(source, "read -r choice < /dev/tty", "choice=1")
				dir := t.TempDir()
				writeFile(t, filepath.Join(dir, "install.sh"), source, 0o644)
				writeFile(t, filepath.Join(dir, "configflow-agent-linux-amd64"), l1CandidateAgent, 0o755)
				var setup strings.Builder
				setup.WriteString("set -eu\n")
				i := 0
				for path, text := range files {
					fixture := fmt.Sprintf("fixture-%d", i)
					i++
					writeFile(t, filepath.Join(dir, fixture), text, 0o600)
					setup.WriteString("mkdir -p " + shlexQuote(filepath.Dir(path)) + "\ncp /srv/l1/" + fixture + " " + shlexQuote(path) + "\n")
					mode := "600"
					if strings.Contains(path, "/bin/") || strings.HasSuffix(path, "/configflow-agent") || strings.HasPrefix(path, "/etc/init.d/") {
						mode = "755"
					}
					setup.WriteString("chmod " + mode + " " + shlexQuote(path) + "\n")
				}
				if manager == "systemd" {
					setup.WriteString("systemctl daemon-reload\n")
				}
				if existing {
					for _, unit := range units {
						if manager == "systemd" {
							setup.WriteString("systemctl enable " + unit + "\nsystemctl start " + unit + "\nsleep 0.2\nsystemctl is-active --quiet " + unit + "\n")
						} else {
							setup.WriteString("rc-update add " + unit + " default\nrc-service " + unit + " start\nsleep 0.2\nrc-service " + unit + " status\nold_supervisor=$(cat /run/" + unit + ".pid)\ngrep -qs \"^PPid:[[:space:]]*${old_supervisor}\\$\" /proc/[0-9]*/status\n")
						}
					}
				}
				setup.WriteString("python3 -m http.server 18751 --bind 127.0.0.1 --directory /srv/l1 >/srv/l1/http.log 2>&1 </dev/null &\ni=0; until curl -fsS http://127.0.0.1:18751/configflow-agent-linux-amd64 >/dev/null; do i=$((i+1)); [ \"$i\" -lt 100 ] || exit 1; sleep 0.1; done\n")
				writeFile(t, filepath.Join(dir, "setup.sh"), setup.String(), 0o644)
				if out, err := l1Docker(t, "exec", name, "mkdir", "-p", "/srv/l1"); err != nil {
					t.Fatalf("mkdir: %v: %s", err, out)
				}
				if out, err := l1Docker(t, "cp", dir+"/.", name+":/srv/l1"); err != nil {
					t.Fatalf("copy: %v: %s", err, out)
				}
				if out, err := l1Docker(t, "exec", name, "sh", "/srv/l1/setup.sh"); err != nil {
					t.Fatalf("prepare real old services: %v: %s", err, out)
				}
				before := l1ContainerSnapshot(t, name, paths)
				out, runErr := l1Docker(t, "exec", name, "sh", "/srv/l1/install.sh")
				if runErr == nil || !strings.Contains(string(out), "已恢复安装前的 Agent 文件、身份和服务状态") {
					t.Fatalf("expected recovered startup failure: %v: %s", runErr, out)
				}
				if reached, err := l1Docker(t, "exec", name, "sh", "-c", "test -s /srv/l1/candidate-starts"); err != nil {
					t.Fatalf("candidate startup fault was not reached: %v: %s", err, reached)
				}
				if aio {
					if reached, err := l1Docker(t, "exec", name, "sh", "-c", "grep -Fq /mihomo/config.json /srv/l1/candidate-starts && grep -Fq /oxidns/config.json /srv/l1/candidate-starts"); err != nil {
						t.Fatalf("both AIO candidates were not started: %v: %s", err, reached)
					}
				}
				if after := l1ContainerSnapshot(t, name, paths); after != before {
					t.Fatalf("files/modes/identities changed after rollback:\nbefore %s\nafter %s", before, after)
				}
				for _, unit := range units {
					activeArgs := []string{"exec", name, "systemctl", "is-active", "--quiet", unit}
					enabledArgs := []string{"exec", name, "systemctl", "is-enabled", "--quiet", unit}
					if manager == "openrc" {
						activeArgs = []string{"exec", name, "rc-service", unit, "status"}
						enabledArgs = []string{"exec", name, "sh", "-c", "test -L \"/etc/runlevels/default/$1\"", "sh", unit}
					}
					if state, err := l1Docker(t, activeArgs...); (err == nil) != existing {
						t.Fatalf("active %s: %v: %s", unit, err, state)
					}
					if state, err := l1Docker(t, enabledArgs...); (err == nil) != existing {
						t.Fatalf("enabled %s: %v: %s", unit, err, state)
					}
				}
				t.Logf("real %s %s: candidate startup failed; old file bytes/modes, identities and service states restored", manager, scenario)
			})
		}
	}
}
