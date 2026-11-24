package preflight

import (
	"agent/clioutput"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// The runtime fixture deliberately exposes only the target PID's procfs files.
// A host-wide interface with the same name is not evidence of core ownership.
type tunRuntimeFixture struct {
	files       map[string][]byte
	fileErrors  map[string]error
	entries     []os.DirEntry
	dirError    error
	statAfter   []byte
	statError   error
	statReads   int
	readPaths   []string
	readDirPath string
	readCounts  map[string]int
	filesAfter  map[string][]byte
	errorsAfter map[string]error
}

type tunRuntimeFDEntry string

func (entry tunRuntimeFDEntry) Name() string               { return string(entry) }
func (entry tunRuntimeFDEntry) IsDir() bool                { return false }
func (entry tunRuntimeFDEntry) Type() os.FileMode          { return 0 }
func (entry tunRuntimeFDEntry) Info() (os.FileInfo, error) { return nil, os.ErrNotExist }

func tunRuntimeStat(pid int, comm, starttime string) []byte {
	// comm is field 2 and may contain spaces and ')'. The fields after its final
	// ')' start at field 3; starttime must therefore be the twentieth such field.
	fields := []string{"S"}
	for field := 4; field <= 21; field++ {
		fields = append(fields, "0")
	}
	fields = append(fields, starttime, "0", "0")
	return []byte(fmt.Sprintf("%d (%s) %s\n", pid, comm, strings.Join(fields, " ")))
}

func tunRuntimeNetDev(names ...string) []byte {
	var out strings.Builder
	out.WriteString("Inter-|   Receive                                                |  Transmit\n")
	out.WriteString(" face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed\n")
	for _, name := range names {
		fmt.Fprintf(&out, " %s: 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0\n", name)
	}
	return []byte(out.String())
}

func newTUNRuntimeFixture() *tunRuntimeFixture {
	return &tunRuntimeFixture{
		files: map[string][]byte{
			"/proc/42/stat":     tunRuntimeStat(42, "mihomo", "123456"),
			"/proc/42/fdinfo/7": []byte("pos:\t0\nflags:\t0100002\niff:\tMeta\n"),
			"/proc/42/net/dev":  tunRuntimeNetDev("lo", "Meta"),
		},
		fileErrors:  map[string]error{},
		entries:     []os.DirEntry{tunRuntimeFDEntry("7")},
		readCounts:  map[string]int{},
		filesAfter:  map[string][]byte{},
		errorsAfter: map[string]error{},
	}
}

func (fixture *tunRuntimeFixture) probe(t *testing.T) systemProbe {
	t.Helper()
	return systemProbe{
		goos: "linux",
		readFile: func(path string) ([]byte, error) {
			fixture.readPaths = append(fixture.readPaths, path)
			fixture.readCounts[path]++
			if fixture.readCounts[path] > 1 {
				if err := fixture.errorsAfter[path]; err != nil {
					return nil, err
				}
				if value, ok := fixture.filesAfter[path]; ok {
					return value, nil
				}
			}
			if path == "/proc/42/stat" {
				fixture.statReads++
				if fixture.statReads > 1 {
					if fixture.statError != nil {
						return nil, fixture.statError
					}
					if fixture.statAfter != nil {
						return fixture.statAfter, nil
					}
				}
			}
			if err := fixture.fileErrors[path]; err != nil {
				return nil, err
			}
			if value, ok := fixture.files[path]; ok {
				return value, nil
			}
			return nil, os.ErrNotExist
		},
		readDir: func(path string) ([]os.DirEntry, error) {
			fixture.readDirPath = path
			if path != "/proc/42/fdinfo" {
				t.Fatalf("unexpected procfs directory: %q", path)
			}
			return fixture.entries, fixture.dirError
		},
		stat: func(path string) (os.FileInfo, error) {
			t.Fatalf("runtime inspection should read procfs evidence, not stat %q", path)
			return nil, os.ErrNotExist
		},
		tun: func(path string) (uint32, error) {
			t.Fatalf("runtime inspection must not open or create a TUN device: %q", path)
			return 0, os.ErrPermission
		},
		showUnit: func(unit string) ([]byte, error) {
			t.Fatalf("runtime inspection already has a target PID; unexpected service operation: %q", unit)
			return nil, os.ErrPermission
		},
	}
}

func requireTUNRuntimeWarning(t *testing.T, check Check, reasonWords ...string) {
	t.Helper()
	if check.Level != "warn" || check.Message == "" {
		t.Fatalf("expected a runtime warning with its reason, got %+v", check)
	}
	for _, word := range reasonWords {
		if strings.Contains(check.Message, word) {
			return
		}
	}
	t.Fatalf("runtime warning has no relevant reason (%q): %+v", reasonWords, check)
}

func TestCoreTUNRuntimeReadsTargetProcessNamespace(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	// The installer's namespace has no Meta. Only the actual core's namespace
	// contains the interface associated with its TUN file descriptor.
	fixture.files["/proc/net/dev"] = tunRuntimeNetDev("lo", "eth0")
	fixture.files["/proc/self/net/dev"] = tunRuntimeNetDev("lo", "eth0")
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	if check.Level != "pass" || !strings.Contains(check.Message, "42") || !strings.Contains(check.Message, "Meta") {
		t.Fatalf("target-process TUN evidence did not pass with PID and interface: %+v", check)
	}
	if fixture.readDirPath != "/proc/42/fdinfo" || fixture.statReads < 2 {
		t.Fatalf("ownership or process identity was not checked: %+v", fixture)
	}
	for _, path := range fixture.readPaths {
		if path == "/proc/net/dev" || path == "/proc/self/net/dev" {
			t.Fatalf("runtime evidence came from the installer's namespace: %v", fixture.readPaths)
		}
	}
}

func TestCoreTUNRuntimeDeduplicatesQueuesAndVerifiesEveryInterface(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	fixture.entries = append(fixture.entries, tunRuntimeFDEntry("8"), tunRuntimeFDEntry("9"))
	fixture.files["/proc/42/fdinfo/8"] = []byte("iff:\tMeta\n")
	fixture.files["/proc/42/fdinfo/9"] = []byte("iff:\ttun1\n")
	fixture.files["/proc/42/net/dev"] = tunRuntimeNetDev("Meta", "tun1")
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	if check.Level != "pass" || strings.Count(check.Message, "Meta") != 1 || strings.Count(check.Message, "tun1") != 1 {
		t.Fatalf("multi-queue descriptors were not reduced to unique interfaces: %+v", check)
	}

	fixture.files["/proc/42/net/dev"] = tunRuntimeNetDev("Meta")
	check = inspectCoreTUNRuntime(fixture.probe(t), 42)
	requireTUNRuntimeWarning(t, check, "tun1")
}

func TestCoreTUNRuntimeRequiresNonemptyFDOwnershipEvidence(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"ordinary-fd-with-unrelated-same-name-interface", "pos:\t0\nflags:\t0100002\n"},
		{"empty-iff", "pos:\t0\niff:\t\n"},
		{"whitespace-iff", "iff:    \t \n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTUNRuntimeFixture()
			fixture.files["/proc/42/fdinfo/7"] = []byte(tc.body)
			check := inspectCoreTUNRuntime(fixture.probe(t), 42)
			requireTUNRuntimeWarning(t, check, "iff", "描述符", "关联", "持有", "接口")
		})
	}
}

func TestCoreTUNRuntimeMatchesInterfaceNamesExactly(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	fixture.files["/proc/42/net/dev"] = tunRuntimeNetDev("lo", "Meta-extra", "otherMeta")
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	requireTUNRuntimeWarning(t, check, "Meta")
}

func TestCoreTUNRuntimeHandlesPartialFDReadFailures(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	fixture.entries = append(fixture.entries, tunRuntimeFDEntry("8"))
	fixture.fileErrors["/proc/42/fdinfo/8"] = os.ErrPermission
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	if check.Level != "pass" || !strings.Contains(check.Message, "Meta") {
		t.Fatalf("unrelated unreadable descriptor hid verified TUN evidence: %+v", check)
	}

	fixture.fileErrors["/proc/42/fdinfo/7"] = os.ErrPermission
	check = inspectCoreTUNRuntime(fixture.probe(t), 42)
	requireTUNRuntimeWarning(t, check, "读取", "权限", "fdinfo", "描述符")
}

func TestCoreTUNRuntimeRechecksOwnershipAfterReadingInterfaceList(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*tunRuntimeFixture)
	}{
		{"fd-closed", func(f *tunRuntimeFixture) { f.errorsAfter["/proc/42/fdinfo/7"] = os.ErrNotExist }},
		{"fd-rebound", func(f *tunRuntimeFixture) { f.filesAfter["/proc/42/fdinfo/7"] = []byte("iff:\tAnother\n") }},
		{"fd-unbound", func(f *tunRuntimeFixture) { f.filesAfter["/proc/42/fdinfo/7"] = []byte("iff:\t\n") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTUNRuntimeFixture()
			tc.mutate(fixture)
			// Meta remains in net/dev after the core closes/replaces the FD.
			// A stable PID and persistent interface must not conceal lost ownership.
			check := inspectCoreTUNRuntime(fixture.probe(t), 42)
			requireTUNRuntimeWarning(t, check, "变化", "持有", "读取", "Meta", "描述符", "关联")
			if fixture.readCounts["/proc/42/fdinfo/7"] < 2 {
				t.Fatalf("TUN ownership was not rechecked: %v", fixture.readPaths)
			}
			netdevIndex, secondFDIndex, fdReads := -1, -1, 0
			for index, path := range fixture.readPaths {
				if path == "/proc/42/net/dev" {
					netdevIndex = index
				}
				if path == "/proc/42/fdinfo/7" {
					fdReads++
					if fdReads == 2 {
						secondFDIndex = index
					}
				}
			}
			if netdevIndex < 0 || secondFDIndex <= netdevIndex {
				t.Fatalf("ownership was not rechecked after the interface snapshot: %v", fixture.readPaths)
			}
		})
	}
}

func TestCoreTUNRuntimeAllowsOneQueueToCloseWhileAnotherStillOwnsInterface(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	fixture.entries = append(fixture.entries, tunRuntimeFDEntry("8"))
	fixture.files["/proc/42/fdinfo/8"] = []byte("iff:\tMeta\n")
	fixture.errorsAfter["/proc/42/fdinfo/7"] = os.ErrNotExist
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	if check.Level != "pass" || strings.Count(check.Message, "Meta") != 1 {
		t.Fatalf("remaining live queue did not establish interface ownership: %+v", check)
	}
	if fixture.readCounts["/proc/42/fdinfo/8"] < 2 {
		t.Fatalf("remaining queue was not checked again: %v", fixture.readPaths)
	}
}

func TestCoreTUNRuntimeReportsUnavailableEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		pid    int
		mutate func(*tunRuntimeFixture)
		words  []string
	}{
		{"no-pid", 0, func(*tunRuntimeFixture) {}, []string{"PID", "进程", "运行"}},
		{"stat-permission", 42, func(f *tunRuntimeFixture) { f.fileErrors["/proc/42/stat"] = os.ErrPermission }, []string{"stat", "进程", "读取", "权限"}},
		{"process-gone-before-check", 42, func(f *tunRuntimeFixture) { f.fileErrors["/proc/42/stat"] = os.ErrNotExist }, []string{"stat", "进程", "读取", "退出"}},
		{"fd-directory-permission", 42, func(f *tunRuntimeFixture) { f.dirError = os.ErrPermission }, []string{"fdinfo", "读取", "权限", "描述符"}},
		{"empty-fd-directory", 42, func(f *tunRuntimeFixture) { f.entries = nil }, []string{"iff", "描述符", "持有", "关联"}},
		{"netdev-permission", 42, func(f *tunRuntimeFixture) { f.fileErrors["/proc/42/net/dev"] = os.ErrPermission }, []string{"net/dev", "读取", "权限", "网络"}},
		{"missing-interface", 42, func(f *tunRuntimeFixture) { f.files["/proc/42/net/dev"] = tunRuntimeNetDev("lo") }, []string{"Meta"}},
		{"process-gone-after-check", 42, func(f *tunRuntimeFixture) { f.statError = os.ErrNotExist }, []string{"进程", "退出", "stat", "读取"}},
		{"pid-reused", 42, func(f *tunRuntimeFixture) { f.statAfter = tunRuntimeStat(42, "mihomo", "999999") }, []string{"进程", "变化", "改变", "starttime"}},
		{"invalid-stat", 42, func(f *tunRuntimeFixture) { f.files["/proc/42/stat"] = []byte("42 (mihomo) S\n") }, []string{"stat", "进程", "解析", "身份"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newTUNRuntimeFixture()
			tc.mutate(fixture)
			check := inspectCoreTUNRuntime(fixture.probe(t), tc.pid)
			requireTUNRuntimeWarning(t, check, tc.words...)
		})
	}
}

func TestCoreTUNRuntimeParsesStatCommWithSpacesAndRightParentheses(t *testing.T) {
	for _, comm := range []string{"mihomo worker", "mihomo) worker", "mihomo (dns) worker)"} {
		t.Run(comm, func(t *testing.T) {
			fixture := newTUNRuntimeFixture()
			fixture.files["/proc/42/stat"] = tunRuntimeStat(42, comm, "123456")
			fixture.statAfter = tunRuntimeStat(42, comm, "123456")
			check := inspectCoreTUNRuntime(fixture.probe(t), 42)
			if check.Level != "pass" {
				t.Fatalf("valid comm confused process identity parsing: %+v", check)
			}
			fixture.statAfter = tunRuntimeStat(42, comm, "654321")
			fixture.statReads = 0
			check = inspectCoreTUNRuntime(fixture.probe(t), 42)
			requireTUNRuntimeWarning(t, check, "进程", "变化", "改变", "starttime")
		})
	}
}

func TestCoreTUNRuntimeReportPrintsObservedResult(t *testing.T) {
	fixture := newTUNRuntimeFixture()
	check := inspectCoreTUNRuntime(fixture.probe(t), 42)
	var out bytes.Buffer
	Report{check}.PrintWithOptions(&out, clioutput.Options{Verbose: true}, false)
	printed := out.String()
	if check.Level != "pass" || !strings.Contains(printed, "[通过]") || !strings.Contains(printed, "42") || !strings.Contains(printed, "Meta") {
		t.Fatalf("report did not print the actual runtime result: %q", printed)
	}
	for _, obsolete := range []string{"本检查未在核心", "预检未验证核心实际创建接口", "详细检查范围见日志"} {
		if strings.Contains(printed, obsolete) {
			t.Fatalf("a fixed unverified-TUN warning contradicted the observed result: %q", printed)
		}
	}
}
