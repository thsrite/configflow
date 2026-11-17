package agents_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestL1ShellDownloadErrorsWithCurlAndShell(t *testing.T) {
	s := newServer(t)
	url := s.URL() + "/api/agents/i/UNKNOWNunknownUNKNOWN0"
	r := s.Get("/api/agents/i/UNKNOWNunknownUNKNOWN0").WantStatus(404)
	if r.Text() != "安装链接不存在或已过期，请在面板重新生成。\n" || r.Header.Get("Content-Type") != "text/plain; charset=utf-8" || r.Header.Get("Content-Disposition") != "" {
		t.Fatalf("shell download error: %s %v", r.Body, r.Header)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	curl := exec.CommandContext(ctx, "curl", "-fsSL", url)
	if out, err := curl.Output(); err == nil || len(out) != 0 {
		t.Fatalf("curl -fsSL must reject error without a script body: %v %q", err, out)
	} else if e, ok := err.(*exec.ExitError); !ok || e.ExitCode() != 22 {
		t.Fatalf("curl failed for another reason: %v", err)
	}
	pipe := exec.CommandContext(ctx, "sh", "-c", `curl -sS "$1" | sh`, "sh", url)
	pipe.Env = append(os.Environ(), "LANG=C.UTF-8", "LC_ALL=C.UTF-8")
	if out, err := pipe.CombinedOutput(); err == nil || !strings.Contains(string(out), "安装链接不存在或已过期") || strings.Contains(string(out), "success") {
		t.Fatalf("curl | sh did not expose plain error text: %v %s", err, out)
	}
}
