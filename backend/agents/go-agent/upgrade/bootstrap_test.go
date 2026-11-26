package upgrade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapWithoutInstalledCore(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, kind := range []string{"mihomo", "mosdns", "oxidns"} {
			for _, binaryCase := range []string{"omitted", "bare-name", "missing-path", "on-path"} {
				t.Run(manager+"/"+kind+"/"+binaryCase, func(t *testing.T) {
					c, root := testConfig(t, manager)
					binary := filepath.Join(root, "agent")
					c.Values["service_type"] = kind
					wantCore := kind
					switch binaryCase {
					case "omitted":
						delete(c.Values, "service_binary")
					case "missing-path":
						wantCore = filepath.Join(root, "not-installed", kind)
						c.Values["service_binary"] = wantCore
					default:
						c.Values["service_binary"] = wantCore
					}
					data, err := json.Marshal(c.Values)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(c.Path, data, 0600); err != nil {
						t.Fatal(err)
					}

					commandsDir := filepath.Join(root, "commands")
					if err := os.Mkdir(commandsDir, 0755); err != nil {
						t.Fatal(err)
					}
					logPath := filepath.Join(root, "manager.log")
					for _, name := range []string{"systemctl", "rc-update", "rc-service"} {
						writeBootstrapCommand(t, filepath.Join(commandsDir, name))
					}
					coreOnPath := filepath.Join(commandsDir, kind)
					coreScript := []byte("#!/bin/sh\nexit 42\n")
					if binaryCase == "on-path" {
						// Lookup is allowed to succeed, but the configured bare
						// name must survive instead of being replaced by this path.
						if err := os.WriteFile(coreOnPath, coreScript, 0755); err != nil {
							t.Fatal(err)
						}
					}
					t.Setenv("PATH", commandsDir)
					t.Setenv("CONFIGFLOW_TEST_MANAGER_LOG", logPath)

					ready, err := migrationReady(c.Path, binary, root)
					if err != nil || ready {
						t.Fatalf("unmigrated installation was ready: ready=%v err=%v", ready, err)
					}
					if err := ensureInstalled(c.Path, binary, root); err != nil {
						t.Fatalf("startup migration failed for %s core: %v", binaryCase, err)
					}
					migrated, err := ReadConfig(c.Path)
					if err != nil {
						t.Fatal(err)
					}
					if migrated.Values["upgrade_schema"] != float64(migrationSchema) {
						t.Fatalf("schema was not persisted: %v", migrated.Values["upgrade_schema"])
					}
					if migrated.String("service_binary") != wantCore {
						t.Fatalf("core name/path changed: got %q, want %q", migrated.String("service_binary"), wantCore)
					}
					if migrated.String("service_manager") != manager || migrated.String("service_unit") != "custom-core" {
						t.Fatalf("inferred lifecycle was not persisted: %v", migrated.Values)
					}
					if migrated.String("agent_id") != "retained-id" || migrated.String("token") != "retained-token" {
						t.Fatal("startup migration lost Agent identity")
					}
					if binaryCase == "on-path" {
						coreData, err := os.ReadFile(coreOnPath)
						if err != nil || !bytes.Equal(coreData, coreScript) {
							t.Fatalf("migration modified PATH core: data=%q err=%v", coreData, err)
						}
					} else if _, err := os.Stat(coreOnPath); !os.IsNotExist(err) {
						t.Fatalf("migration created a core binary: %v", err)
					}
					if binaryCase == "missing-path" {
						if _, err := os.Stat(wantCore); !os.IsNotExist(err) {
							t.Fatalf("migration created the missing configured core: %v", err)
						}
					}
					if _, err := os.Stat(c.String("config_path")); !os.IsNotExist(err) {
						t.Fatalf("migration created a core configuration: %v", err)
					}

					gate := "configflow-recover-" + kind
					gatePath := filepath.Join(root, "etc/init.d", gate)
					dependencyPath := filepath.Join(root, "etc/conf.d/custom-core")
					wantCommands := "rc-update -u\n"
					if manager == "systemd" {
						gatePath = filepath.Join(root, "etc/systemd/system", gate+".service")
						dependencyPath = filepath.Join(root, "etc/systemd/system/custom-core.service.d/configflow-recovery.conf")
						wantCommands = "systemctl daemon-reload\n"
					}
					for _, path := range []string{gatePath, dependencyPath} {
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Fatalf("migration created startup recovery file %s: %v", path, err)
						}
					}
					commandLog, err := os.ReadFile(logPath)
					if err != nil || string(commandLog) != wantCommands {
						t.Fatalf("unexpected service manager calls: got %q, want %q; err=%v", commandLog, wantCommands, err)
					}
					ready, err = migrationReady(c.Path, binary, root)
					if err != nil || !ready {
						t.Fatalf("independent core startup was not ready: ready=%v err=%v", ready, err)
					}
					journalData, err := os.ReadFile(filepath.Join(root, ".agent-migration.json"))
					if err != nil {
						t.Fatal(err)
					}
					var journal bootstrapJournal
					if err := json.Unmarshal(journalData, &journal); err != nil || !journal.Complete {
						t.Fatalf("migration journal was not completed: journal=%+v err=%v", journal, err)
					}

					if err := ensureInstalled(c.Path, binary, root); err != nil {
						t.Fatalf("repeat startup failed: %v", err)
					}
					secondLog, err := os.ReadFile(logPath)
					if err != nil || !bytes.Equal(secondLog, commandLog) {
						t.Fatalf("repeat startup reran the service manager: %q; err=%v", secondLog, err)
					}

					if err := AtomicWrite(gatePath, []byte("legacy recovery gate"), 0644); err != nil {
						t.Fatal(err)
					}
					ready, err = migrationReady(c.Path, binary, root)
					if err != nil || ready {
						t.Fatalf("reintroduced recovery gate was ready: ready=%v err=%v", ready, err)
					}
				})
			}
		}
	}
}

func writeBootstrapCommand(t *testing.T, path string) {
	t.Helper()
	// Shell builtins keep PATH fully isolated from host service managers and
	// host core binaries, while recording the exact manager arguments.
	script := "#!/bin/sh\n" +
		"printf '%s' \"${0##*/}\" >> \"$CONFIGFLOW_TEST_MANAGER_LOG\"\n" +
		"for arg in \"$@\"; do printf ' %s' \"$arg\" >> \"$CONFIGFLOW_TEST_MANAGER_LOG\"; done\n" +
		"printf '\\n' >> \"$CONFIGFLOW_TEST_MANAGER_LOG\"\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestCommandLifecycleSkipsLegacyMigration(t *testing.T) {
	for _, kind := range []string{"mihomo", "mosdns", "oxidns", "custom"} {
		for _, schema := range []interface{}{nil, float64(1), float64(migrationSchema)} {
			t.Run(fmt.Sprintf("%s/schema-%v", kind, schema), func(t *testing.T) {
				c, root := testConfig(t, "systemd")
				c.Values["service_type"] = kind
				c.Values["service_manager"] = "command"
				c.Values["restart_command"] = "custom-core restart"
				c.Values["start_command"] = "custom-core start"
				c.Values["stop_command"] = "custom-core stop"
				c.Values["status_command"] = "custom-core status"
				if schema != nil {
					c.Values["upgrade_schema"] = schema
				}
				original, err := json.Marshal(c.Values)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(c.Path, original, 0600); err != nil {
					t.Fatal(err)
				}
				// No manager or custom lifecycle command may run at startup.
				t.Setenv("PATH", t.TempDir())
				binary := filepath.Join(root, "agent")
				for attempt := 0; attempt < 2; attempt++ {
					if err := ensureInstalled(c.Path, binary, root); err != nil {
						t.Fatal(err)
					}
					if ready, err := migrationReady(c.Path, binary, root); err != nil || !ready {
						t.Fatalf("command lifecycle not ready: %v %v", ready, err)
					}
					got, err := os.ReadFile(c.Path)
					if err != nil || !bytes.Equal(got, original) {
						t.Fatalf("changed custom config: %s %v", got, err)
					}
					if _, err := os.Stat(filepath.Join(root, ".agent-migration.json")); !os.IsNotExist(err) {
						t.Fatalf("created legacy migration state: %v", err)
					}
					// Shell reinstall/upgrade regenerates the config without a schema.
					if err := os.WriteFile(c.Path, original, 0600); err != nil {
						t.Fatal(err)
					}
				}
				// Startup readiness must not enable an unsupported online worker.
				req := Request{ID: "1234567890123456", Version: Version, SHA256: strings.Repeat("a", 64), Size: 12}
				if _, err := Prepare(c.Path, binary, req); err == nil {
					t.Fatal("accepted custom lifecycle online upgrade")
				}
			})
		}
	}
}
