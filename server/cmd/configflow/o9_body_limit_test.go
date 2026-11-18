package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Promoted from TestO9ReproBackendPortHasNoBodyLimit, now exercising the
// actual service listener rather than an unwrapped App test fixture.
func TestO9ReproBackendPortHasNoBodyLimit(t *testing.T) {
	p := startService(t, "idle")
	var address string
	waitFor(t, func() bool {
		data, err := os.ReadFile(filepath.Join(p.dir, "test-ready"))
		return err == nil && json.Unmarshal(data, &address) == nil
	})
	client := &http.Client{Timeout: 10 * time.Second}
	big := append(bytes.Repeat([]byte(" "), 8<<20), []byte(`{"username":"x","password":"y"}`)...)
	for _, chunked := range []bool{false, true} {
		req, err := http.NewRequest("POST", "http://"+address+"/api/auth/login", bytes.NewReader(big))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if chunked {
			req.ContentLength = -1
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != 413 {
			t.Errorf("chunked=%v: 8 MiB login body accepted: %d", chunked, res.StatusCode)
		}
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.wait(t)
}
