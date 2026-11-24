package server

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apiagents "configflow/internal/api/agents"
	apisetup "configflow/internal/api/setup"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/routers"
	"configflow/internal/rulesource"
)

func TestSetupInstallationReadOwnersDoNotInitializeRegistry(t *testing.T) {
	srv := httpxtest.New(t, httpxtest.Options{Auth: true})
	owners := buildSetupOwners(nil, rulesource.New(rulesource.Options{Repo: srv.Repo}), routers.New(srv.Repo, routers.Options{}))
	apisetup.NewHandler(srv.Repo, &owners).Register(srv.App.Router)
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo}).Register(srv.App.Router)
	admin := srv.Login()
	const id = "cccccccccccccccccccccccccccccccc"
	const profile = "setup_cccccccccccccccccccc"
	srv.Post("/api/setup", httpxtest.JSON(map[string]string{"id": id, "name": "Synthetic", "goal": "gateway"}), admin).WantStatus(200)
	// If a read starts the manager, its real legacy import will reject this
	// invalid database. Neither a frozen script nor abandon occupancy needs it.
	legacy := filepath.Join(srv.DataDir, "runtime.sqlite3")
	sentinel := []byte("synthetic invalid legacy database")
	if err := os.WriteFile(legacy, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(srv.DataDir, "gateway-installations")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	record := installation.Record{ID: strings.Repeat("d", 32), ProfileID: profile, Name: "synthetic-linux", Token: "synthetic-installation-capability", Network: host.Spec{Interface: "eth0", Address: "192.0.2.10", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}, Ports: map[string]int{"mihomo": 8080, "oxidns": 8082}, AgentIDs: map[string]string{}, Registered: map[string]string{}, Deployments: map[string]json.RawMessage{}, Expires: float64(time.Now().Add(time.Hour).Unix()), Phase: "waiting", Script: "#!/bin/sh\n# synthetic frozen script\n"}
	b, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, record.ID+".json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	srv.Get("/api/agents/gateway-installations/" + record.ID + "/script?token=" + record.Token).WantStatus(200)
	result := srv.Post("/api/setup/"+id+"/abandon", httpxtest.JSON(map[string]bool{"dry_run": true}), admin).WantStatus(200)
	if strings.Contains(string(result.Body), `"delete_profile":true`) {
		t.Fatal("installation occupancy was ignored")
	}
	after, err := os.ReadFile(legacy)
	if err != nil || !bytes.Equal(after, sentinel) {
		t.Fatal("read initialized or changed legacy runtime", err)
	}
}
