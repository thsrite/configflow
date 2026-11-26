package provider_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/provider"
	"configflow/internal/repo"
	"configflow/internal/store"
	"configflow/internal/testgolden"
)

const (
	provOldContent   = "proxies: [{name: OldOne}, {name: OldTwo}]\n"
	provFreshContent = "proxies: [{name: Fresh}]\n"
)

func TestSlowProviderGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	for _, mode := range []string{"production", "fresh"} {
		for _, path := range []string{"/api/aggregations/a/provider", "/api/profiles/alpha/aggregations/a/provider"} {
			label := "global"
			if strings.Contains(path, "/profiles/") {
				label = "profile"
			}
			t.Run(mode+"/"+label, func(t *testing.T) {
				started, release := make(chan struct{}), make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				defer unblock()

				srv := httpxtest.New(t, httpxtest.Options{
					Setup: func(app *httpx.App) {
						service := provider.New(provider.Options{Repo: app.Repo})
						if mode == "fresh" {
							t.Cleanup(service.SetResponseSeconds(-1))
						}
						app.Router.GET(path, "synthetic.provider", func(c *httpx.Ctx) (*httpx.Response, error) {
							return service.Serve(c, "aggregation", "a", "alpha", &store.Head{StoreID: mode + "-" + label}, func(context.Context) (*httpx.Response, error) {
								close(started)
								<-release
								return httpx.Bytes(200, "text/yaml; charset=utf-8", []byte(provFreshContent)), nil
							})
						})
					},
				})
				if _, err := srv.Repo.CreateProfileRecord(srv.Context(), repo.ProfileCreation{ID: "alpha", Name: "Alpha"}, ""); err != nil {
					t.Fatal(err)
				}
				if _, err := srv.Repo.WriteProfileText(srv.Context(), "alpha", "providers/a.yaml", []byte(provOldContent)); err != nil {
					t.Fatal(err)
				}
				type answer struct {
					Status int    `json:"status"`
					Body   string `json:"body"`
					Marker string `json:"marker"`
					Stored string `json:"stored"`
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
					timer := time.AfterFunc(150*time.Millisecond, unblock)
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
					if err == nil && string(raw) == provFreshContent {
						actual.Stored = string(raw)
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("render did not finish publishing", err)
					}
					time.Sleep(time.Millisecond)
				}
				key, _ := json.Marshal(map[string]string{"mode": mode, "label": label})
				val, _ := json.Marshal(actual)
				ref.Assert(t, key, val)
			})
		}
	}
}
