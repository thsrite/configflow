// Package difftest compares delayed provider responses with the real Python owner.
package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/provider"
	"configflow/internal/repo"
)

const oldContent = "proxies: [{name: OldOne}, {name: OldTwo}]\n"
const freshContent = "proxies: [{name: Fresh}]\n"

type answer struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Marker string `json:"marker"`
	Stored string `json:"stored"`
}

func TestSlowProviderParity(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	for _, mode := range []string{"production", "fresh"} {
		for _, path := range []string{"/api/aggregations/a/provider", "/api/profiles/alpha/aggregations/a/provider"} {
			label := "global"
			if strings.Contains(path, "/profiles/") {
				label = "profile"
			}
			t.Run(mode+"/"+label, func(t *testing.T) {
				expected := python(t, mode, path)
				if mode == "fresh" {
					t.Cleanup(provider.SetResponseSeconds(-1))
				}
				started, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()
				srv := httpxtest.New(t, httpxtest.Options{Register: httpxtest.Routes(func(r *httpx.Router) {
					r.GET(path, "synthetic.provider", func(c *httpx.Ctx) (*httpx.Response, error) {
						return provider.Serve(c, "aggregation", "a", "alpha", &model.Config{Profile: &model.Profile{ID: t.Name()}}, func(context.Context) (*httpx.Response, error) {
							close(started)
							<-release
							return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(freshContent)), nil
						})
					})
				})})
				if _, err := srv.Repo.CreateProfileRecord(srv.Context(), repo.ProfileCreation{ID: "alpha", Name: "Alpha"}, ""); err != nil {
					t.Fatal(err)
				}
				if _, err := srv.Repo.WriteProfileText(srv.Context(), "alpha", "providers/a.yaml", []byte(oldContent)); err != nil {
					t.Fatal(err)
				}
				done := make(chan answer, 1)
				go func() {
					reply := srv.Get(path)
					done <- answer{Status: reply.Status, Body: reply.Text(), Marker: reply.Header.Get("X-ConfigFlow-Provider")}
				}()
				select {
				case <-started:
				case <-time.After(time.Minute):
					t.Fatal("render did not start")
				}
				if mode == "fresh" {
					timer := time.AfterFunc(250*time.Millisecond, unblock)
					defer timer.Stop()
				}
				var actual answer
				select {
				case actual = <-done:
				case <-time.After(time.Minute):
					t.Fatal("provider did not answer")
				}
				unblock()
				stored, err := srv.Repo.ProfilePath("alpha", "providers/served/aggregation-a.yaml")
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for {
					raw, err := os.ReadFile(stored)
					if err == nil && string(raw) == freshContent {
						actual.Stored = string(raw)
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("render did not finish publishing", err)
					}
					time.Sleep(time.Millisecond)
				}
				if actual != expected {
					t.Fatalf("Python=%+v Go=%+v", expected, actual)
				}
				wantBody, wantMarker := oldContent, "last-good"
				if mode == "fresh" {
					wantBody, wantMarker = freshContent, ""
				}
				if actual.Status != 200 || actual.Body != wantBody || actual.Marker != wantMarker {
					t.Fatalf("wrong response mode: %+v", actual)
				}
			})
		}
	}
}

func python(t *testing.T, mode, path string) answer {
	t.Helper()
	executable := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if executable == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		executable = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("testdata/slow.py")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"directory": t.TempDir(), "mode": mode, "path": path})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Python: %v %s", err, stderr.String())
	}
	var result answer
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("Python reply: %v %s", err, out)
	}
	return result
}
