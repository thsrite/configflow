package svc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseSvstat(t *testing.T) {
	tests := []struct {
		name   string
		out    string
		fatal  bool
		want   State
		pid    int
		uptime time.Duration
	}{
		{"running", "true true 42 120\n", false, StateRunning, 42, 120 * time.Second},
		{"starting within start period", "true true 42 2", false, StateStarting, 42, 2 * time.Second},
		{"start period reached", "true true 42 3", false, StateRunning, 42, 3 * time.Second},
		{"stopping", "true false 42 50", false, StateStopping, 42, 50 * time.Second},
		{"restarting after exit", "false true -1 0", false, StateBackoff, 0, 0},
		{"stopped", "false false -1 7", false, StateStopped, 0, 0},
		{"fatal", "false true -1 9", true, StateFatal, 0, 0},
		{"fatal and wanted down", "false false -1 9", true, StateFatal, 0, 0},
		{"marker ignored while up", "true true 5 30", true, StateRunning, 5, 30 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSvstat("mihomo", tt.out, tt.fatal, 3*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			want := Status{Name: "mihomo", State: tt.want, PID: tt.pid, Uptime: tt.uptime}
			if got != want {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestParseSvstatRejectsUnexpectedOutput(t *testing.T) {
	for _, out := range []string{"", "true true 1", "yes true 1 2", "true true x 2", "true true 1 -1", "s6-svstat: fatal: unable to read status"} {
		if _, err := ParseSvstat("oxidns", out, false, time.Second); err == nil {
			t.Errorf("%q: accepted", out)
		}
	}
}

func TestStatusLine(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{Status{Name: "oxidns", State: StateRunning, PID: 12, Uptime: 3723 * time.Second}, "oxidns RUNNING pid 12, uptime 1:02:03"},
		{Status{Name: "oxidns", State: StateFatal}, "oxidns FATAL"},
		{Status{Name: "configflow-keepalived", State: StateStopped}, "configflow-keepalived STOPPED"},
	}
	for _, tt := range tests {
		if got := strings.Join(strings.Fields(tt.status.String()), " "); got != tt.want {
			t.Errorf("got %q, want %q", got, tt.want)
		}
		// The panel's startup check matches 'oxidns '*RUNNING*.
		if tt.status.State == StateRunning && !strings.HasPrefix(tt.status.String(), tt.status.Name+" ") {
			t.Errorf("status line must start with the name and a space: %q", tt.status.String())
		}
	}
}

// fakeS6 simulates s6-supervise for the tools S6 runs.
type fakeS6 struct {
	mu       sync.Mutex
	up       map[string]bool
	wanted   map[string]bool
	since    map[string]time.Time
	fail     map[string]bool // goes FATAL instead of starting
	stateDir string
	calls    []string
}

func newFake(t *testing.T) (*fakeS6, *S6) {
	t.Helper()
	root := t.TempDir()
	scandir := filepath.Join(root, "service")
	state := filepath.Join(root, "state")
	for _, name := range []string{"mihomo", Keepalived} {
		if err := os.MkdirAll(filepath.Join(scandir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &fakeS6{up: map[string]bool{}, wanted: map[string]bool{}, since: map[string]time.Time{}, fail: map[string]bool{}, stateDir: state}
	return f, &S6{Scandir: scandir, CommandDir: "/command", StateDir: state, Run: f.run, Poll: time.Millisecond}
}

func (f *fakeS6) run(_ context.Context, name string, args ...string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	program := filepath.Base(args[len(args)-1])
	f.calls = append(f.calls, filepath.Base(name)+" "+strings.Join(args[:len(args)-1], " ")+" "+program)
	switch filepath.Base(name) {
	case "s6-svc":
		switch args[0] {
		case "-u":
			f.wanted[program] = true
			if f.fail[program] {
				os.WriteFile(fatalFile(f.stateDir, program), nil, 0o600)
			} else {
				f.up[program] = true
				// Started long enough ago to be past the start period.
				f.since[program] = time.Now().Add(-time.Hour)
			}
		case "-d":
			f.wanted[program], f.up[program] = false, false
		}
		return nil, nil
	case "s6-svstat":
		seconds := int(time.Since(f.since[program]) / time.Second)
		if !f.up[program] {
			seconds = 0
		}
		return []byte(fmt.Sprintf("%t %t %d %d\n", f.up[program], f.wanted[program], map[bool]int{true: 77, false: -1}[f.up[program]], seconds)), nil
	}
	return nil, fmt.Errorf("unexpected tool %s", name)
}

func TestS6Lifecycle(t *testing.T) {
	ctx := context.Background()
	fake, m := newFake(t)
	os.WriteFile(fatalFile(m.StateDir, "mihomo"), nil, 0o600)
	os.WriteFile(failuresFile(m.StateDir, "mihomo"), []byte("2"), 0o600)
	if err := m.Start(ctx, "mihomo"); err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, path := range []string{fatalFile(m.StateDir, "mihomo"), failuresFile(m.StateDir, "mihomo")} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("start kept %s", filepath.Base(path))
		}
	}
	status, err := m.Status(ctx, "mihomo")
	if err != nil || status.State != StateRunning || status.PID != 77 {
		t.Fatalf("status after start: %+v %v", status, err)
	}
	if err := m.Restart(ctx, "mihomo"); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := m.Stop(ctx, "mihomo"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if status, _ := m.Status(ctx, "mihomo"); status.State != StateStopped {
		t.Fatalf("status after stop: %+v", status)
	}
	// Stopping a stopped program succeeds, as the deployment code expects.
	if err := m.Stop(ctx, "mihomo"); err != nil {
		t.Fatalf("second stop: %v", err)
	}
	fake.fail["mihomo"] = true
	if err := m.Start(ctx, "mihomo"); !errors.Is(err, ErrFatal) {
		t.Fatalf("start of a failing program: %v", err)
	}
	if status, _ := m.Status(ctx, "mihomo"); status.State != StateFatal {
		t.Fatalf("status of a failing program: %+v", status)
	}
}

func TestS6UnknownPrograms(t *testing.T) {
	_, m := newFake(t)
	ctx := context.Background()
	for _, name := range []string{"mosdns" /* defined, not in this scandir */, "sshd" /* not a program */} {
		if _, err := m.Status(ctx, name); !errors.Is(err, ErrUnknown) {
			t.Errorf("status %s: %v", name, err)
		}
		if err := m.Start(ctx, name); !errors.Is(err, ErrUnknown) {
			t.Errorf("start %s: %v", name, err)
		}
	}
}

func TestStopTimesOutWhileProgramStaysUp(t *testing.T) {
	_, m := newFake(t)
	m.Run = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if filepath.Base(name) == "s6-svstat" {
			return []byte("true false 9 40"), nil
		}
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := m.Stop(ctx, "mihomo")
	if err == nil || !strings.Contains(err.Error(), "STOPPING") {
		t.Fatalf("stop of a program that stays up: %v", err)
	}
}

func TestControlCommand(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		args   []string
		prep   func(*fakeS6, *S6)
		code   int
		stdout string
		stderr string
	}{
		{"status running", []string{"status", "mihomo"}, func(f *fakeS6, m *S6) { m.Start(ctx, "mihomo") }, 0, "mihomo RUNNING pid 77", ""},
		{"status stopped", []string{"status", "mihomo"}, nil, exitNotRunning, "mihomo STOPPED", ""},
		{"status unknown", []string{"status", "sshd"}, nil, exitUnknown, "sshd：本容器没有这个受管程序", ""},
		{"start", []string{"start", "mihomo"}, nil, 0, "mihomo：已启动", ""},
		{"stop", []string{"stop", "mihomo"}, nil, 0, "mihomo：已停止", ""},
		{"fatal start", []string{"start", "mihomo"}, func(f *fakeS6, _ *S6) { f.fail["mihomo"] = true }, 1, "", "mihomo 启动后多次很快退出"},
		{"bad action", []string{"reload", "mihomo"}, nil, 2, "", msgUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, m := newFake(t)
			if tt.prep != nil {
				tt.prep(fake, m)
			}
			var stdout, stderr bytes.Buffer
			code := control(ctx, m, tt.args[0], tt.args[1], &stdout, &stderr)
			if code != tt.code {
				t.Fatalf("code %d, want %d (stdout %q stderr %q)", code, tt.code, stdout.String(), stderr.String())
			}
			if got := strings.Join(strings.Fields(stdout.String()), " "); !strings.HasPrefix(got, tt.stdout) {
				t.Errorf("stdout %q, want prefix %q", got, tt.stdout)
			}
			if !strings.Contains(stderr.String(), tt.stderr) {
				t.Errorf("stderr %q, want %q", stderr.String(), tt.stderr)
			}
		})
	}
}

func TestFinishReproducesStartRetries(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tests := []struct {
		name      string
		wanted    string
		uptime    time.Duration // since the recorded start; <0 means no record
		failures  string
		code      int
		failAfter string // failures file afterwards ("" = absent)
		fatal     bool
	}{
		{"requested stop", "false", time.Second, "2", 0, "2", false},
		{"crash after start period", "true", 10 * time.Second, "2", 0, "", false},
		{"first quick exit", "true", time.Second, "", 0, "1", false},
		{"third quick exit", "true", time.Second, "2", 0, "3", false},
		{"fourth quick exit is fatal", "true", time.Second, "3", finishPermanent, "", true},
		{"no start record counts as quick", "true", -1, "", 0, "1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.uptime >= 0 {
				os.WriteFile(startedFile(dir, "mihomo"), []byte(fmt.Sprint(now.Add(-tt.uptime).UnixMilli())), 0o600)
			}
			if tt.failures != "" {
				os.WriteFile(failuresFile(dir, "mihomo"), []byte(tt.failures), 0o600)
			}
			f := &Finisher{StateDir: dir, CommandDir: "/command", Now: func() time.Time { return now },
				Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
					if filepath.Base(name) != "s6-svstat" || args[1] != "wantedup" {
						t.Fatalf("unexpected call %s %v", name, args)
					}
					return []byte(tt.wanted + "\n"), nil
				}}
			if code := f.Finish(context.Background(), "mihomo", "/run/service/mihomo"); code != tt.code {
				t.Fatalf("code %d, want %d", code, tt.code)
			}
			raw, _ := os.ReadFile(failuresFile(dir, "mihomo"))
			if string(raw) != tt.failAfter {
				t.Errorf("failures %q, want %q", raw, tt.failAfter)
			}
			if _, err := os.Stat(fatalFile(dir, "mihomo")); (err == nil) != tt.fatal {
				t.Errorf("fatal marker present=%t, want %t", err == nil, tt.fatal)
			}
		})
	}
}

func TestProgramOverride(t *testing.T) {
	dir := t.TempDir()
	argv, err := Program(dir, "oxidns")
	if err != nil || !reflect.DeepEqual(argv, Specs["oxidns"].Argv) {
		t.Fatalf("default: %v %v", argv, err)
	}
	os.WriteFile(argvFile(dir, "oxidns"), []byte(`["/usr/local/bin/oxidns","start","-c","/data/a b%.yaml","-d","/data"]`), 0o600)
	argv, err = Program(dir, "oxidns")
	if err != nil || argv[3] != "/data/a b%.yaml" {
		t.Fatalf("override: %v %v", argv, err)
	}
	for _, bad := range []string{`[]`, `["oxidns"]`, `{`} {
		os.WriteFile(argvFile(dir, "oxidns"), []byte(bad), 0o600)
		if _, err := Program(dir, "oxidns"); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if _, err := Program(dir, "sshd"); !errors.Is(err, ErrUnknown) {
		t.Errorf("unknown program: %v", err)
	}
}

func TestCoreArgv(t *testing.T) {
	tests := []struct {
		core, config string
		want         []string
	}{
		{"mihomo", "/etc/mihomo/config.yaml", []string{"/usr/local/bin/mihomo", "-d", "/etc/mihomo", "-f", "/etc/mihomo/config.yaml"}},
		{"mosdns", "/data/m/c.yaml", []string{"/usr/local/bin/mosdns", "start", "-c", "/data/m/c.yaml", "-d", "/data/m"}},
		{"oxidns", "/etc/oxidns/config.yaml", []string{"/usr/local/bin/oxidns", "start", "-c", "/etc/oxidns/config.yaml", "-d", "/etc/oxidns"}},
	}
	for _, tt := range tests {
		if got := CoreArgv(tt.core, tt.config); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: %v", tt.core, got)
		}
	}
}

// tree lists every file below dir with its content and mode.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		raw, _ := os.ReadFile(path)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = fmt.Sprintf("%o %s", info.Mode().Perm(), raw)
		return nil
	})
	return files
}

func testLayout(t *testing.T) Layout {
	root := t.TempDir()
	return Layout{SourceDir: filepath.Join(root, "s6-rc.d"), BundleDir: filepath.Join(root, "bundle"),
		StateDir: filepath.Join(root, "state"), LogRoot: filepath.Join(root, "log")}
}

func TestSetupMainOxiDNSMacvlan(t *testing.T) {
	layout := testLayout(t)
	// Leftovers of an earlier start must go.
	os.MkdirAll(filepath.Join(layout.SourceDir, "old-program"), 0o755)
	os.MkdirAll(layout.StateDir, 0o700)
	os.WriteFile(fatalFile(layout.StateDir, "oxidns"), nil, 0o600)
	argv := map[string][]string{"oxidns": CoreArgv("oxidns", "/data/config.yaml")}
	if err := Setup(layout, MainPlan([]string{"oxidns", "agent-oxidns"}, argv, true)); err != nil {
		t.Fatal(err)
	}
	src := tree(t, layout.SourceDir)
	checks := map[string]string{
		"oxidns/type":                            "644 longrun\n",
		"oxidns/run":                             "755 #!/bin/sh\nexec /command/with-contenv /usr/local/bin/configflow-agent svc-exec oxidns\n",
		"oxidns/finish":                          "755 #!/bin/sh\nexec /usr/local/bin/configflow-agent svc-finish oxidns\n",
		"oxidns/timeout-kill":                    "644 10000\n",
		"oxidns/producer-for":                    "644 oxidns-log\n",
		"oxidns-log/consumer-for":                "644 oxidns\n",
		"oxidns-log/run":                         "755 #!/bin/sh\nexec /command/s6-log n3 s10485760 " + filepath.Join(layout.LogRoot, "oxidns") + "\n",
		"agent-oxidns/dependencies.d/oxidns":     "644 ",
		"agent-oxidns/timeout-kill":              "644 10000\n",
		Keepalived + "/timeout-kill":             "644 15000\n",
		"ha-release/type":                        "644 oneshot\n",
		"ha-release/down":                        "644 /usr/local/bin/configflow-agent svc stop configflow-keepalived\n",
		"ha-release/dependencies.d/agent-oxidns": "644 ",
	}
	for path, want := range checks {
		if src[path] != want {
			t.Errorf("%s = %q, want %q", path, src[path], want)
		}
	}
	for _, absent := range []string{"old-program/type", CompanionServ + "/type", Keepalived + "/producer-for", "agent-oxidns/producer-for"} {
		if _, ok := src[absent]; ok {
			t.Errorf("%s should not exist", absent)
		}
	}
	bundle := tree(t, layout.BundleDir)
	var names []string
	for name := range bundle {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{"agent-oxidns", "ha-release", "oxidns", "oxidns-log"}; !reflect.DeepEqual(names, want) {
		t.Errorf("bundle %v, want %v", names, want)
	}
	state := tree(t, layout.StateDir)
	if len(state) != 1 || state["oxidns.argv"] != `600 ["/usr/local/bin/oxidns","start","-c","/data/config.yaml","-d","/data"]` {
		t.Errorf("state %v", state)
	}
	if info, err := os.Stat(filepath.Join(layout.LogRoot, "oxidns")); err != nil || !info.IsDir() {
		t.Errorf("log directory: %v", err)
	}
}

func TestSetupPlans(t *testing.T) {
	tests := []struct {
		name   string
		plan   Plan
		bundle []string
		absent []string
	}{
		{"aio mihomo and mosdns", MainPlan([]string{"mihomo", "agent-mihomo", "mosdns", "agent-mosdns"}, nil, false),
			[]string{"agent-mihomo", "agent-mosdns", "mihomo", "mihomo-log", "mosdns", "mosdns-log"}, []string{"ha-release/type"}},
		{"host-mode HA main", MainPlan([]string{"oxidns", "agent-oxidns"}, nil, false),
			[]string{"agent-oxidns", "oxidns", "oxidns-log"}, []string{"ha-release/type"}},
		{"companion", CompanionPlan(),
			[]string{"ha-companion-release", CompanionServ}, []string{"oxidns/type", "agent-oxidns/type", "ha-companion-release/dependencies.d/agent-oxidns"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := testLayout(t)
			if err := Setup(layout, tt.plan); err != nil {
				t.Fatal(err)
			}
			var names []string
			for name := range tree(t, layout.BundleDir) {
				names = append(names, name)
			}
			sort.Strings(names)
			if !reflect.DeepEqual(names, tt.bundle) {
				t.Errorf("bundle %v, want %v", names, tt.bundle)
			}
			src := tree(t, layout.SourceDir)
			for _, path := range tt.absent {
				if _, ok := src[path]; ok {
					t.Errorf("%s should not exist", path)
				}
			}
		})
	}
}

func TestSetupRejectsInconsistentPlans(t *testing.T) {
	for _, plan := range []Plan{
		{Defined: []string{"sshd"}},
		{Defined: []string{"mihomo"}, Enabled: []string{"mosdns"}},
		{Defined: []string{"mihomo"}, Argv: map[string][]string{"oxidns": {"/x"}}},
	} {
		if err := Setup(testLayout(t), plan); err == nil {
			t.Errorf("%+v accepted", plan)
		}
	}
}

func TestRemoteControl(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake, local := newFake(t)
	socket := filepath.Join(t.TempDir(), "control.sock")
	listener, err := Listen(socket)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(socket); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode: %v %v", info, err)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, listener, local) }()
	remote := Remote{Socket: socket}

	// A liveness probe that connects and closes must not disturb the server.
	if conn, err := net.Dial("unix", socket); err == nil {
		conn.Close()
	}
	if status, err := remote.Status(ctx, Keepalived); err != nil || status.State != StateStopped {
		t.Fatalf("status: %+v %v", status, err)
	}
	if err := remote.Start(ctx, Keepalived); err != nil {
		t.Fatalf("start: %v", err)
	}
	if status, _ := remote.Status(ctx, Keepalived); status.State != StateRunning || status.PID != 77 {
		t.Fatalf("status after start: %+v", status)
	}
	if err := remote.Restart(ctx, Keepalived); err != nil {
		t.Fatalf("restart: %v", err)
	}
	if err := remote.Stop(ctx, Keepalived); err != nil {
		t.Fatalf("stop: %v", err)
	}
	fake.fail[Keepalived] = true
	if err := remote.Start(ctx, Keepalived); !errors.Is(err, ErrFatal) {
		t.Fatalf("fatal start: %v", err)
	}
	// Only keepalived is reachable through the companion.
	if _, err := remote.Status(ctx, "oxidns"); !errors.Is(err, ErrUnknown) {
		t.Fatalf("other program: %v", err)
	}
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(conn, "shutdown\n")
	line := make([]byte, 256)
	n, _ := conn.Read(line)
	conn.Close()
	if !strings.Contains(string(line[:n]), "unsupported command") {
		t.Fatalf("unsupported command reply: %q", line[:n])
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("serve: %v", err)
	}
	if _, err := (Remote{Socket: socket}).Status(context.Background(), Keepalived); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("closed socket: %v", err)
	}
}

func TestListenReplacesOnlyStaleSockets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "control.sock")
	first, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	// net.UnixListener.Close removes the file; recreate a stale socket file.
	stale, _ := net.Listen("unix", path)
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	second, err := Listen(path)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	second.Close()
	regular := filepath.Join(dir, "file")
	os.WriteFile(regular, nil, 0o600)
	if _, err := Listen(regular); err == nil {
		t.Fatal("a regular file was replaced")
	}
}

func TestManagerValues(t *testing.T) {
	tests := []struct {
		in, normal string
		container  bool
	}{
		{"s6", "s6", true},
		{"supervisor", "s6", true},
		{"systemd", "systemd", false},
		{"openrc", "openrc", false},
		{"", "", false},
	}
	for _, tt := range tests {
		if got := NormalizeManager(tt.in); got != tt.normal || IsContainerManager(tt.in) != tt.container {
			t.Errorf("%q: %q %t", tt.in, got, IsContainerManager(tt.in))
		}
	}
}
