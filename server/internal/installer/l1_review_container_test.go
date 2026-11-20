package installer

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Independent L1 review failure injection with real systemd (dash, bash) and
// OpenRC (BusyBox ash). Each case owns one container with no network.

const l1ReviewCandidate = `#!/bin/sh
# L1_REVIEW_CANDIDATE
for argument do
    case "$argument" in -version|-validate-service|-check-environment|-check-deployments|-require-clean-deployments) exit 0 ;; esac
done
printf '%s\n' "$*" >> /srv/l1/candidate-starts
# The real Agent writes its startup-migration journal next to its config.
if [ -d "${2%/*}" ]; then touch "${2%/*}/.agent-migration.json"; fi
if [ -f /srv/l1/fault-hook ]; then sh /srv/l1/fault-hook; fi
if [ -f /srv/l1/fail ]; then
    case "$*" in *$(cat /srv/l1/fail)*) echo 'L1 review candidate fault' >&2; exit 37 ;; esac
fi
exec sleep 100000
`

// l1ReviewDriver runs the installer in its own session, like a terminal job,
// and injects a signal at a point observed from the outside.
const l1ReviewDriver = `import os, signal, subprocess, sys, time
mode, interpreter, script = sys.argv[1:4]
def count(path):
    try:
        with open(path) as f: return len(f.read().splitlines())
    except FileNotFoundError: return 0
old_base = count('/srv/l1/old-starts')
out = open('/srv/l1/install.out', 'wb')
p = subprocess.Popen([interpreter, script], start_new_session=True, stdin=subprocess.DEVNULL, stdout=out, stderr=subprocess.STDOUT)
def wait_for(pred, timeout=120):
    end = time.time() + timeout
    while time.time() < end:
        if pred(): return True
        if p.poll() is not None: return False
        time.sleep(0.02)
    return False
sent = False
if mode == 'int-after-start':
    if wait_for(lambda: count('/srv/l1/candidate-starts') >= 1):
        time.sleep(0.3); os.killpg(p.pid, signal.SIGINT); sent = True
elif mode == 'term-during-restore':
    if wait_for(lambda: count('/srv/l1/old-starts') > old_base):
        sent = True
        for _ in range(8):
            if p.poll() is not None: break
            os.killpg(p.pid, signal.SIGTERM); time.sleep(0.15)
elif mode == 'kill-after-start':
    if wait_for(lambda: count('/srv/l1/candidate-starts') >= 1):
        os.killpg(p.pid, signal.SIGKILL); sent = True
rc = p.wait(timeout=240)
print('SENT' if sent else 'MISSED', rc)
`

type l1ReviewBox struct {
	t             *testing.T
	name, manager string
	dir           string
	setup         strings.Builder
	n             int
}

func l1ReviewStart(t *testing.T, image, manager string) *l1ReviewBox {
	t.Helper()
	b := &l1ReviewBox{t: t, name: "cfl1r-" + strings.ToLower(rand.Text()[:12]), manager: manager, dir: t.TempDir()}
	args := []string{"run", "-d", "--name", b.name, "--label", "configflow.l1-installer=review", "--network", "none", "--tmpfs", "/run", "--tmpfs", "/tmp"}
	if manager == "systemd" {
		args = append(args, "--privileged", "--cgroupns=host", "-v", "/sys/fs/cgroup:/sys/fs/cgroup:rw", "--tmpfs", "/run/lock")
	} else {
		args = append(args, "--init")
	}
	t.Cleanup(func() {
		if out, err := l1Docker(t, "rm", "-f", b.name); err != nil && !strings.Contains(string(out), "No such container") {
			t.Errorf("remove owned container: %v: %s", err, out)
		}
	})
	if out, err := l1Docker(t, append(args, image)...); err != nil {
		t.Fatalf("start init container: %v: %s", err, out)
	}
	t.Logf("owned container %s", b.name)
	if manager == "systemd" {
		if out, err := b.exec("i=0; until systemctl list-units >/dev/null 2>&1; do i=$((i+1)); [ \"$i\" -lt 100 ] || exit 1; sleep 0.1; done"); err != nil {
			t.Fatalf("systemd did not boot: %v: %s", err, out)
		}
	}
	b.setup.WriteString("set -eu\nmkdir -p /srv/l1\n")
	return b
}

func (b *l1ReviewBox) exec(script string) (string, error) {
	out, err := l1Docker(b.t, "exec", b.name, "sh", "-c", script)
	return string(out), err
}

func (b *l1ReviewBox) must(script string) string {
	b.t.Helper()
	out, err := b.exec(script)
	if err != nil {
		b.t.Fatalf("%s: %v: %s", script, err, out)
	}
	return out
}

// file stages one fixture; mode "" picks 0755 for executables, else 0600.
func (b *l1ReviewBox) file(path, body, mode string) {
	fixture := fmt.Sprintf("fixture-%d", b.n)
	b.n++
	writeFile(b.t, filepath.Join(b.dir, fixture), body, 0o600)
	if mode == "" {
		mode = "600"
		if strings.Contains(path, "/bin/") || strings.HasSuffix(path, "/configflow-agent") || strings.HasPrefix(path, "/etc/init.d/") {
			mode = "755"
		}
	}
	fmt.Fprintf(&b.setup, "mkdir -p %s\ncp /srv/l1/%s %s\nchmod %s %s\n", shlexQuote(filepath.Dir(path)), fixture, shlexQuote(path), mode, shlexQuote(path))
}

func (b *l1ReviewBox) line(s string) { b.setup.WriteString(s + "\n") }

func (b *l1ReviewBox) unitPath(unit string) string {
	if b.manager == "systemd" {
		return "/etc/systemd/system/" + unit + ".service"
	}
	return "/etc/init.d/" + unit
}

// cores installs inert core services, as the earlier acceptance did.
func (b *l1ReviewBox) cores() []string {
	var paths []string
	for _, kind := range []string{"mihomo", "oxidns"} {
		b.file("/usr/local/bin/"+kind, "#!/bin/sh\nexit 0\n", "")
		b.file("/etc/"+kind+"/config.yaml", "# L1 unchanged core config\n", "")
		b.file(b.unitPath(kind), l1Unit(b.manager, kind, "/bin/sleep", "100000"), "")
		paths = append(paths, "/usr/local/bin/"+kind, "/etc/"+kind+"/config.yaml", b.unitPath(kind))
	}
	return paths
}

// oldAgent installs and starts a running, enabled previous Agent.
func (b *l1ReviewBox) oldAgent(unit, binary, config, kind string, aio bool) []string {
	b.file(binary, l1OldAgent, "")
	b.file(b.unitPath(unit), l1Unit(b.manager, unit, binary, config), "")
	b.file(config, fmt.Sprintf("{\"agent_name\":\"old-%s\",\"agent_id\":\"old-%s-id\",\"token\":\"old-%s-token\",\"service_type\":\"%s\",\"service_unit\":\"%s\",\"service_manager\":\"%s\",\"agent_service_unit\":\"%s\",\"native_aio\":%t,\"config_path\":\"/etc/%s/config.yaml\"}\n",
		kind, kind, kind, kind, kind, b.manager, unit, aio, kind), "")
	return []string{binary, config, b.unitPath(unit)}
}

func (b *l1ReviewBox) startOld(units ...string) {
	if b.manager == "systemd" {
		b.line("systemctl daemon-reload")
	}
	for _, unit := range units {
		if b.manager == "systemd" {
			b.line("systemctl enable " + unit + "\nsystemctl start " + unit + "\nsleep 0.2\nsystemctl is-active --quiet " + unit)
		} else {
			b.line("rc-update add " + unit + " default\nrc-service " + unit + " start\nsleep 0.2\nrc-service " + unit + " status")
		}
	}
}

func (b *l1ReviewBox) script(name, source string) {
	b.file("/srv/l1/"+name, strings.ReplaceAll(source, "read -r choice < /dev/tty", "choice=1"), "644")
}

// commit copies fixtures in, runs setup and serves the candidate binary.
func (b *l1ReviewBox) commit() {
	b.t.Helper()
	writeFile(b.t, filepath.Join(b.dir, "configflow-agent-linux-amd64"), l1ReviewCandidate, 0o755)
	writeFile(b.t, filepath.Join(b.dir, "drive.py"), l1ReviewDriver, 0o644)
	b.line("python3 -m http.server 18751 --bind 127.0.0.1 --directory /srv/l1 >/srv/l1/http.log 2>&1 </dev/null &\ni=0; until curl -fsS http://127.0.0.1:18751/configflow-agent-linux-amd64 >/dev/null; do i=$((i+1)); [ \"$i\" -lt 100 ] || exit 1; sleep 0.1; done")
	writeFile(b.t, filepath.Join(b.dir, "setup.sh"), b.setup.String(), 0o644)
	b.must("mkdir -p /srv/l1")
	if out, err := l1Docker(b.t, "cp", b.dir+"/.", b.name+":/srv/l1"); err != nil {
		b.t.Fatalf("copy: %v: %s", err, out)
	}
	b.must("sh /srv/l1/setup.sh")
}

func (b *l1ReviewBox) snapshot(paths []string) string {
	return l1ContainerSnapshot(b.t, b.name, paths)
}

func (b *l1ReviewBox) drive(mode, interpreter, script string) (string, string) {
	b.t.Helper()
	status := b.must("python3 /srv/l1/drive.py " + mode + " " + interpreter + " /srv/l1/" + script)
	out := b.must("cat /srv/l1/install.out")
	if !strings.HasPrefix(status, "SENT ") {
		b.t.Fatalf("%s: injection point not reached: %s\n%s", mode, status, out)
	}
	return strings.TrimSpace(strings.TrimPrefix(status, "SENT ")), out
}

func (b *l1ReviewBox) state(unit string) (active, enabled bool) {
	if b.manager == "systemd" {
		_, err := b.exec("systemctl is-active --quiet " + unit)
		_, err2 := b.exec("systemctl is-enabled --quiet " + unit)
		return err == nil, err2 == nil
	}
	_, err := b.exec("rc-service " + unit + " status")
	_, err2 := b.exec("test -L /etc/runlevels/default/" + unit)
	return err == nil, err2 == nil
}

func (b *l1ReviewBox) wantState(unit string, active, enabled bool) {
	b.t.Helper()
	if a, e := b.state(unit); a != active || e != enabled {
		b.t.Fatalf("%s: active=%v enabled=%v, want %v/%v", unit, a, e, active, enabled)
	}
}

// agentProcesses counts "sleep 100000" processes: old and candidate Agents
// both end in it (the OpenRC image's PID 1 too, hence compare with a baseline).
func (b *l1ReviewBox) agentProcesses() string {
	return strings.TrimSpace(b.must("pgrep -f '^sleep 100000$' | wc -l"))
}

func l1ReviewSingle(t *testing.T, name string) string {
	t.Helper()
	const server = "http://127.0.0.1:18751"
	source, err := RenderAgentInstall(AgentInstallOptions{AgentPort: 8080, ServerURL: server, AgentName: name, ServiceType: "oxidns", BinaryDownloadURL: server, ManageIPv4Forwarding: ForwardingOff})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func l1ReviewAIO(t *testing.T) string {
	t.Helper()
	const server = "http://127.0.0.1:18751"
	o := DefaultAIOOptions(server, "l1-review-aio")
	o.BinaryDownloadURL, o.ManageIPv4Forwarding, o.ManageTun = server, false, false
	source, err := RenderAIOInstall(o)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

const (
	l1ReviewRestored    = "已恢复安装前的 Agent 文件、身份和服务状态"
	l1ReviewSingleBin   = "/usr/local/bin/configflow-agent"
	l1ReviewSingleCfg   = "/opt/configflow-agent/config-oxidns.json"
	l1ReviewSingleUnit  = "configflow-agent"
	l1ReviewLeftoverMsg = "发现以前安装中断或恢复失败时保留的 Agent 备份："
)

func TestL1ReviewFailureInjectionWithRealInitManagers(t *testing.T) {
	if os.Getenv("CONFIGFLOW_L1_CONTAINERS") != "1" {
		t.Skip("set CONFIGFLOW_L1_CONTAINERS=1 on the test server")
	}
	for _, manager := range []string{"systemd", "openrc"} {
		image := l1InitImage(t, manager)
		shell := map[string]string{"systemd": "dash", "openrc": "busybox-ash"}[manager]

		// SIGINT (Ctrl-C to the whole job) while the new Agent is starting.
		t.Run(manager+"/sigint-during-start/"+shell, func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			paths := append(b.cores(), b.oldAgent(l1ReviewSingleUnit, l1ReviewSingleBin, l1ReviewSingleCfg, "oxidns", false)...)
			b.startOld(l1ReviewSingleUnit)
			b.script("install.sh", l1ReviewSingle(t, "l1-replacement"))
			b.commit()
			before := b.snapshot(paths)
			processes := b.agentProcesses()
			rc, out := b.drive("int-after-start", "sh", "install.sh")
			t.Logf("exit %s\n%s", rc, out)
			if rc != "130" || !strings.Contains(out, l1ReviewRestored) {
				t.Fatalf("interrupted install did not compensate: exit %s", rc)
			}
			if after := b.snapshot(paths); after != before {
				t.Fatalf("files changed:\n%s\n%s", before, after)
			}
			b.wantState(l1ReviewSingleUnit, true, true)
			if n := b.agentProcesses(); n != processes {
				t.Fatalf("Agent processes %s after compensation, %s before", n, processes)
			}
		})

		// Repeated SIGTERM while compensation restarts the old Agent.
		t.Run(manager+"/sigterm-during-restore/"+shell, func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			paths := append(b.cores(), b.oldAgent(l1ReviewSingleUnit, l1ReviewSingleBin, l1ReviewSingleCfg, "oxidns", false)...)
			b.startOld(l1ReviewSingleUnit)
			b.line("printf '%s' -config > /srv/l1/fail")
			b.script("install.sh", l1ReviewSingle(t, "l1-replacement"))
			b.commit()
			before := b.snapshot(paths)
			rc, out := b.drive("term-during-restore", "sh", "install.sh")
			t.Logf("exit %s\n%s", rc, out)
			if rc != "1" || !strings.Contains(out, l1ReviewRestored) {
				t.Fatalf("termination interrupted compensation: exit %s", rc)
			}
			if after := b.snapshot(paths); after != before {
				t.Fatalf("files changed:\n%s\n%s", before, after)
			}
			b.wantState(l1ReviewSingleUnit, true, true)
		})

		// Power loss after the files were replaced, then the same link again.
		t.Run(manager+"/sigkill-then-rerun/"+shell, func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			b.cores()
			b.oldAgent(l1ReviewSingleUnit, l1ReviewSingleBin, l1ReviewSingleCfg, "oxidns", false)
			b.startOld(l1ReviewSingleUnit)
			b.script("install.sh", l1ReviewSingle(t, "l1-replacement"))
			b.commit()
			oldConfig := b.must("sha256sum " + l1ReviewSingleCfg + " | cut -d' ' -f1")
			rc, out := b.drive("kill-after-start", "sh", "install.sh")
			t.Logf("killed run exit %s\n%s", rc, out)
			leftover := strings.TrimSpace(b.must("ls -d /opt/configflow-agent/.agent-install-backup.*"))
			if strings.Contains(leftover, "\n") || leftover == "" {
				t.Fatalf("expected exactly one leftover journal: %q", leftover)
			}
			// The leftover journal still holds the old identity byte for byte.
			kept := b.must("for p in " + leftover + "/path.*; do [ \"$(cat \"$p\")\" = " + l1ReviewSingleCfg + " ] && sha256sum \"" + leftover + "/file.${p##*.}\" | cut -d' ' -f1; done; true")
			if strings.TrimSpace(kept) != strings.TrimSpace(oldConfig) {
				t.Fatalf("leftover journal lost the old identity: %q vs %q", kept, oldConfig)
			}
			b.must("printf '%s' -config > /srv/l1/fail")
			rerun, err := b.exec("sh /srv/l1/install.sh 2>&1")
			t.Logf("rerun: %v\n%s", err, rerun)
			if err == nil || !strings.Contains(rerun, l1ReviewLeftoverMsg+leftover) {
				t.Fatalf("rerun did not report the leftover journal")
			}
			// The rerun restarts the half-installed candidate it found, which
			// still fails here, so it may keep its own journal too; the
			// leftover one must survive unchanged and is never applied.
			again := b.must("for p in " + leftover + "/path.*; do [ \"$(cat \"$p\")\" = " + l1ReviewSingleCfg + " ] && sha256sum \"" + leftover + "/file.${p##*.}\" | cut -d' ' -f1; done; true")
			if strings.TrimSpace(again) != strings.TrimSpace(oldConfig) {
				t.Fatalf("rerun changed the leftover journal: %q", again)
			}
			if current := b.must("sha256sum " + l1ReviewSingleCfg + " | cut -d' ' -f1"); current == oldConfig {
				t.Fatal("a leftover journal was applied automatically")
			}
		})

		// A failed fresh AIO install must not block a later single install.
		t.Run(manager+"/aio-new-then-single/"+shell, func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			paths := b.cores()
			for _, kind := range []string{"mihomo", "oxidns"} {
				paths = append(paths, "/opt/configflow-agent/"+kind, b.unitPath("configflow-agent-"+kind))
			}
			b.line("printf '%s' /oxidns/config.json > /srv/l1/fail")
			b.script("aio.sh", l1ReviewAIO(t))
			b.script("single.sh", l1ReviewSingle(t, "l1-after-aio"))
			b.commit()
			before := b.snapshot(paths)
			out, err := b.exec("sh /srv/l1/aio.sh 2>&1")
			t.Logf("aio: %v\n%s", err, out)
			if err == nil || !strings.Contains(out, l1ReviewRestored) {
				t.Fatal("failed AIO did not compensate")
			}
			b.must("grep -Fq /mihomo/config.json /srv/l1/candidate-starts && grep -Fq /oxidns/config.json /srv/l1/candidate-starts")
			if after := b.snapshot(paths); after != before {
				t.Fatalf("fresh AIO left files:\n%s\n%s", before, after)
			}
			b.must("rm -f /srv/l1/fail")
			out, err = b.exec("sh /srv/l1/single.sh 2>&1")
			t.Logf("single: %v\n%s", err, out)
			if err != nil || strings.Contains(out, "检测到原生同机双 Agent") {
				t.Fatal("single install after failed AIO did not succeed")
			}
			b.wantState(l1ReviewSingleUnit, true, true)
		})

		// Recovery gate, drop-in names with space and $, and OpenRC symlinks.
		interpreter := map[string]string{"systemd": "bash", "openrc": "sh"}[manager]
		t.Run(manager+"/gate-restore/"+map[string]string{"systemd": "bash", "openrc": shell}[manager], func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			paths := append(b.cores(), b.oldAgent(l1ReviewSingleUnit, l1ReviewSingleBin, l1ReviewSingleCfg, "oxidns", false)...)
			if manager == "systemd" {
				gate := "/etc/systemd/system/configflow-recover-oxidns.service"
				dropin := "/etc/systemd/system/oxidns.service.d/my $HOME override.conf"
				agentDropin := "/etc/systemd/system/configflow-agent.service.d/custom.conf"
				b.file(gate, "[Unit]\nDescription=L1 gate\n[Service]\nType=oneshot\nExecStart=/bin/true\nRemainAfterExit=yes\n", "644")
				b.file(dropin, "[Unit]\nRequires=configflow-recover-oxidns.service\nAfter=network.target \\\n configflow-recover-oxidns.service\n[Service]\nEnvironment=KEEP=1\n", "644")
				b.file(agentDropin, "[Unit]\nWants=l1-keep.service configflow-recover-oxidns.service\n[Service]\nEnvironment=EXTRA=1\n", "644")
				paths = append(paths, gate, dropin, agentDropin)
			} else {
				gate, link := "/etc/init.d/configflow-recover-oxidns", "/etc/runlevels/boot/configflow-recover-oxidns"
				conf := "/etc/conf.d/oxidns"
				b.file(gate, "#!/sbin/openrc-run\ndescription=\"L1 gate\"\nstart() { :; }\n", "")
				b.file(conf, "CUSTOM='a $b'\n# BEGIN CONFIGFLOW RECOVERY oxidns\nrc_need=\"${rc_need:-} configflow-recover-oxidns\"\n# END CONFIGFLOW RECOVERY oxidns\n", "644")
				b.line("ln -s " + gate + " " + link)
				paths = append(paths, gate, link, conf)
			}
			b.startOld(l1ReviewSingleUnit)
			b.line("printf '%s' -config > /srv/l1/fail")
			b.script("install.sh", l1ReviewSingle(t, "l1-replacement"))
			b.commit()
			before := b.snapshot(paths)
			out, err := b.exec(interpreter + " /srv/l1/install.sh 2>&1")
			t.Logf("%v\n%s", err, out)
			if err == nil || !strings.Contains(out, l1ReviewRestored) {
				t.Fatal("failed install did not compensate")
			}
			if after := b.snapshot(paths); after != before {
				t.Fatalf("gate/drop-in not restored:\n%s\n%s", before, after)
			}
			b.wantState(l1ReviewSingleUnit, true, true)
		})

		if manager != "systemd" {
			continue
		}
		// Restore itself fails: the binary directory becomes read-only.
		t.Run(manager+"/restore-fails-readonly/"+shell, func(t *testing.T) {
			b := l1ReviewStart(t, image, manager)
			b.cores()
			b.oldAgent(l1ReviewSingleUnit, l1ReviewSingleBin, l1ReviewSingleCfg, "oxidns", false)
			b.startOld(l1ReviewSingleUnit)
			b.line("printf '%s' -config > /srv/l1/fail")
			b.file("/srv/l1/fault-hook", "grep -q ' /usr/local/bin ' /proc/mounts || { mount --bind /usr/local/bin /usr/local/bin && mount -o remount,bind,ro /usr/local/bin; }\n", "644")
			b.script("install.sh", l1ReviewSingle(t, "l1-replacement"))
			b.commit()
			oldSums := b.must("sha256sum " + l1ReviewSingleBin + " " + l1ReviewSingleCfg + " | cut -d' ' -f1")
			out, err := b.exec("sh /srv/l1/install.sh 2>&1")
			t.Logf("%v\n%s", err, out)
			if err == nil || strings.Contains(out, l1ReviewRestored) ||
				!strings.Contains(out, "Agent 文件或服务配置未能全部恢复；服务保持停止") {
				t.Fatal("failed restore was not reported honestly")
			}
			kept := regexp.MustCompile(`Agent 安装备份已保留：(/opt/configflow-agent/\.agent-install-backup\.[A-Za-z0-9]+)`).FindStringSubmatch(out)
			if kept == nil {
				t.Fatal("kept backup path not reported")
			}
			b.wantState(l1ReviewSingleUnit, false, false)
			sums := b.must("for target in " + l1ReviewSingleBin + " " + l1ReviewSingleCfg + "; do for p in " + kept[1] + "/path.*; do [ \"$(cat \"$p\")\" = \"$target\" ] && sha256sum \"" + kept[1] + "/file.${p##*.}\" | cut -d' ' -f1; done; done; true")
			if sums != oldSums {
				t.Fatalf("kept backup differs from the old binary/identity:\n%s\n%s", oldSums, sums)
			}
			b.must("umount /usr/local/bin")
		})
	}
}
