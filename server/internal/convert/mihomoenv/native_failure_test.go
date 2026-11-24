package mihomoenv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/substore"
)

func TestNativeOptionalConversionFailureKeepsClientGeneration(t *testing.T) {
	const source = "unsupported://synthetic-password@node.example.com:443#Unavailable"
	var downloads atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/download/") {
			body, err := io.ReadAll(r.Body)
			if err != nil || r.Method != "GET" || len(body) != 0 || r.URL.Query().Get("content") != source {
				t.Error("unexpected conversion request")
			}
			downloads.Add(1)
			http.Error(w, "conversion unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"success","data":{}}`)
	}))
	defer backend.Close()
	root := t.TempDir()
	environment := substore.NewEnv()
	environment.DataDir = func() string { return root }
	environment.LookupEnv = func(key string) (string, bool) {
		switch key {
		case "SUB_STORE_URL":
			return backend.URL, true
		case "SUB_STORE_BUILTIN_DIR":
			return filepath.Join(root, "missing-bundle"), true
		default:
			return "", false
		}
	}
	sub := substore.NewService(environment)
	t.Cleanup(convert.OverrideMihomoEnvironmentForTest(t, NativeEnvFor(nil, sub, nil)))
	config := &model.Config{Profile: &model.Profile{}, ProfileID: "default", Nodes: []model.Node{{ID: "unavailable", Name: "Unavailable", ProxyString: new(source)}}}
	for _, test := range []struct {
		name     string
		generate func(context.Context, *model.Config, string) (string, error)
	}{
		{"Surge", convert.SurgeConfig}, {"Loon", convert.LoonConfig},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.generate(context.Background(), config, "https://example.com")
			if err != nil {
				t.Fatal("optional conversion blocked client generation", err)
			}
			if !strings.Contains(result, "[General]") || strings.Contains(result, "synthetic-password") {
				t.Fatal("invalid client output after absent optional conversion")
			}
		})
	}
	if downloads.Load() != 2 {
		t.Fatalf("expected both real conversion probes, got %d", downloads.Load())
	}
}
