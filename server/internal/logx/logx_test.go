package logx

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// test_log_text_neutralizes_line_breaks_and_bounds_length (backend/test_operation_logs.py).
func TestLogTextNeutralizesLineBreaksAndBoundsLength(t *testing.T) {
	if got := LogText("a\r\nb\u2028c\td", 120); got != "a??b?c?d" {
		t.Errorf("got %q", got)
	}
	if got := LogText(strings.Repeat("x", 200), 10); got != strings.Repeat("x", 10)+"…" {
		t.Errorf("got %q", got)
	}
	if got := LogText(nil, 120); got != "None" {
		t.Errorf("got %q", got)
	}
	if got := LogText(strings.Repeat("例", 10), 10); got != strings.Repeat("例", 10) {
		t.Errorf("exactly the limit must not be cut: %q", got)
	}
	if got := LogText("a b​c de\x7f", 120); got != "a?b?c?d?e?" {
		t.Errorf("got %q", got)
	}
	if got := LogText(42, 120); got != "42" {
		t.Errorf("got %q", got)
	}
}

func TestIsPrintableMatchesPython(t *testing.T) {
	printable := []rune{' ', 'a', '~', 'é', '例', '🚀', '­' + 0, 0xFFFD, 0x20AC}
	for _, r := range printable {
		if r == 0xAD {
			continue // soft hyphen is Cf: not printable
		}
		if !IsPrintable(r) {
			t.Errorf("%U should be printable", r)
		}
	}
	for _, r := range []rune{0, '\n', '\t', 0x7f, 0x85, 0xa0, 0xad, 0x200b, 0x2028, 0x2029, 0xe000, 0x378, 0xfeff, 0x10ffff} {
		if IsPrintable(r) {
			t.Errorf("%U should not be printable", r)
		}
	}
}

func TestParseLevel(t *testing.T) {
	cases := map[string]Level{"debug": Debug, "INFO": Info, "Warning": Warning, "error": Error, "CRITICAL": Critical, "WARN": Info, "": Info, "trace": Info}
	for name, want := range cases {
		if got := ParseLevel(name); got != want {
			t.Errorf("%q: %v", name, got)
		}
	}
}

func TestFormatLine(t *testing.T) {
	at := time.Date(2026, 11, 14, 9, 5, 7, 42_999_999, time.Local)
	if got := FormatLine(at, "backend.routes.agents", Warning, "x - y"); got != "2026-11-14 09:05:07,042 - backend.routes.agents - WARNING - x - y" {
		t.Errorf("got %q", got)
	}
}

func TestSetupWritesFileAndConsoleAtTheConfiguredLevel(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 6_000_000, time.Local)
	root, err := Setup(Config{Level: Warning, File: filepath.Join(dir, "sub", "app.log"), Console: &console, Clock: func() time.Time { return fixed }})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	log := Get("backend.x")
	log.Info("hidden")
	log.Debugf("hidden %d", 1)
	log.Warningf("shown %s", "中文")
	log.Error("bad")
	want := "2026-01-02 03:04:05,006 - backend.x - WARNING - shown 中文\n2026-01-02 03:04:05,006 - backend.x - ERROR - bad\n"
	data, err := os.ReadFile(filepath.Join(dir, "sub", "app.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want || console.String() != want {
		t.Errorf("file %q console %q", data, console.String())
	}
	if log.Enabled(Info) || !log.Enabled(Error) {
		t.Error("Enabled disagrees with the level")
	}
}

func TestRotationKeepsFiveBackups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")
	root, err := Setup(Config{Level: Info, File: path, MaxBytes: 100, Backups: 3, Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.Local) }})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	log := Get("n")
	for i := 0; i < 20; i++ {
		log.Infof("line %02d", i)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "app.log,app.log.1,app.log.2,app.log.3" {
		t.Fatalf("files: %v", names)
	}
	current, _ := os.ReadFile(path)
	first, _ := os.ReadFile(path + ".1")
	if !strings.HasSuffix(string(current), "line 19\n") || !regexp.MustCompile(`line \d\d\n$`).Match(first) {
		t.Errorf("current %q first backup %q", current, first)
	}
	for _, f := range []string{path, path + ".1", path + ".2", path + ".3"} {
		info, _ := os.Stat(f)
		if info.Size() >= 100 {
			t.Errorf("%s has %d bytes, the limit is exclusive", f, info.Size())
		}
	}
}

func TestClientIP(t *testing.T) {
	if got := ClientIP(nil); got != "-" {
		t.Errorf("no request: %q", got)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "203.0.113.9:5555"
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Errorf("socket address: %q", got)
	}
	r.Header.Set("X-Real-IP", "198.51.100.7\r\nforged")
	if got := ClientIP(r); got != "198.51.100.7??forged" {
		t.Errorf("header: %q", got)
	}
	r.Header.Set("X-Real-IP", strings.Repeat("9", 100))
	if got := ClientIP(r); got != strings.Repeat("9", 64)+"…" {
		t.Errorf("bounded: %q", got)
	}
}

func TestDefaultFile(t *testing.T) {
	t.Setenv("LOG_FILE", "")
	if got := DefaultFile("/data"); got != "/data/app.log" {
		t.Error(got)
	}
	t.Setenv("LOG_FILE", "/tmp/x.log")
	if got := DefaultFile("/data"); got != "/tmp/x.log" {
		t.Error(got)
	}
}

func TestDebugEnabledFromEnv(t *testing.T) {
	t.Setenv("LOG_LEVEL", "debug")
	if !IsDebugEnabled() || LevelFromEnv() != Debug {
		t.Error("debug not detected")
	}
	t.Setenv("LOG_LEVEL", "")
	if IsDebugEnabled() || LevelFromEnv() != Info {
		t.Error("default must be INFO")
	}
}

// Regression (review C02): Werkzeug reads X-Real-IP as latin-1 text, so
// UTF-8 bytes are two characters and a raw 0x85 byte (NEL) is not printable.
func TestClientIPHeaderIsLatin1(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Real-IP", "\xc3\xa9\x85x")
	if got, want := ClientIP(r), "Ã©?x"; got != want {
		t.Errorf("ClientIP = %q, want %q", got, want)
	}
}
