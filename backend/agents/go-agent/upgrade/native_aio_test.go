package upgrade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeAIOMigrationPreservesIndependentInstances(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) {
			root := t.TempDir()
			paths := map[string]string{}
			for _, kind := range []string{"mihomo", "oxidns"} {
				dir := filepath.Join(root, "opt/configflow-agent", kind)
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				binary := filepath.Join(dir, "configflow-agent")
				paths[kind] = binary
				c := &Config{Path: filepath.Join(dir, "config.json"), Values: map[string]any{
					"native_aio": true, "deployment_method": "shell", "service_type": kind,
					"config_path": "/etc/" + kind + "/config.yaml", "service_manager": manager, "service_unit": kind,
					"agent_service_unit": "configflow-agent-" + kind, "agent_id": "id-" + kind, "token": "token-" + kind,
					"agent_port": float64(8080),
				}}
				plan, err := MigrationPlan(c, binary, root)
				if err != nil {
					t.Fatal(err)
				}
				for _, change := range plan {
					if strings.Contains(change.Path, "configflow-agent/") && change.Path != c.Path {
						t.Fatalf("migration touches another instance: %s", change.Path)
					}
				}
				if err := ApplyChanges(plan); err != nil {
					t.Fatal(err)
				}
				loaded, err := ReadConfig(c.Path)
				if err != nil {
					t.Fatal(err)
				}
				if loaded.Values["native_aio"] != true || loaded.AgentUnit() != "configflow-agent-"+kind || loaded.String("agent_id") != "id-"+kind || loaded.String("token") != "token-"+kind {
					t.Fatalf("migration lost native instance metadata: %+v", loaded.Values)
				}
				job := &Job{Status: Status{ID: "native_aio_update_" + kind, AgentID: "id-" + kind}, Binary: binary, AgentUnit: loaded.AgentUnit()}
				if err := job.save(CurrentPath(binary), "queued"); err != nil {
					t.Fatal(err)
				}
			}
			if CurrentPath(paths["mihomo"]) == CurrentPath(paths["oxidns"]) {
				t.Fatal("AIO update journals share a path")
			}
			for kind, binary := range paths {
				data, err := os.ReadFile(CurrentPath(binary))
				if err != nil {
					t.Fatal(err)
				}
				var saved Job
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				if saved.AgentID != "id-"+kind || saved.AgentUnit != "configflow-agent-"+kind || saved.Binary != binary {
					t.Fatalf("update state crossed instances: %+v", saved)
				}
			}
		})
	}
}
