package installer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/installer/installertest"
)

const backendAgents = "../../../backend/agents"

func TestScriptsMatchBackend(t *testing.T) {
	backend, err := filepath.Glob(filepath.Join(backendAgents, "scripts", "*.sh"))
	if err != nil || len(backend) == 0 {
		t.Fatalf("backend/agents/scripts not found: %v", err)
	}
	embedded, _ := scriptFiles.ReadDir("scripts")
	if len(embedded) != len(backend)+1 {
		t.Fatalf("server/internal/installer/scripts has %d files, backend/agents/scripts %d", len(embedded), len(backend))
	}
	wantModule, err := installertest.RollbackModule()
	if err != nil || agentInstallRollback != wantModule {
		t.Fatalf("L1 D3 rollback helper differs from its approved source delta: %v", err)
	}
	for _, path := range backend {
		want, _ := os.ReadFile(path)
		got, err := scriptFiles.ReadFile("scripts/" + filepath.Base(path))
		baseline, baselineErr := installertest.TemplateBaseline(filepath.Base(path), string(got))
		if err != nil || baselineErr != nil || !bytes.Equal([]byte(baseline), want) {
			t.Errorf("server/internal/installer/scripts/%s differs from backend/agents/scripts", filepath.Base(path))
		}
	}
}

// pythonFingerprint is generator_fingerprint() over the backend tree. Paths
// sort like PosixPath objects: component by component.
func pythonFingerprint(t *testing.T, directory string) string {
	var paths []string
	scripts, _ := filepath.Glob(filepath.Join(directory, "scripts", "*.sh"))
	installers, _ := filepath.Glob(filepath.Join(directory, "*install*.py"))
	paths = append(append(append(paths, scripts...), installers...), filepath.Join(directory, "docker_ha_guide.py"))
	sort.Slice(paths, func(i, j int) bool {
		a, b := strings.Split(paths[i], "/"), strings.Split(paths[j], "/")
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	digest := sha256.New()
	digest.Write([]byte(agents.LatestAgentVersion))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || filepath.Base(path) == "install_command_store.py" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest.Write([]byte(filepath.Base(path)))
		digest.Write([]byte{0})
		digest.Write(data)
	}
	return hex.EncodeToString(digest.Sum(nil))[:32]
}

func TestGeneratorFingerprintMatchesReleaseInputs(t *testing.T) {
	if legacyInstallerFingerprint != installertest.LegacyGeneratorFingerprint {
		t.Fatal("legacy fingerprint reference drift")
	}
	if got := pythonFingerprint(t, backendAgents); got != legacyInstallerFingerprint {
		t.Fatalf("original release inputs changed: got %s, expected %s", got, legacyInstallerFingerprint)
	}
	digest := sha256.New()
	digest.Write([]byte(legacyInstallerFingerprint))
	for _, name := range []string{"agent-install-rollback.sh", "cli-output.sh", "install-go-aio.sh", "install-go.sh"} {
		data, err := scriptFiles.ReadFile("scripts/" + name)
		if err != nil {
			t.Fatal(err)
		}
		digest.Write([]byte{0})
		digest.Write([]byte(name))
		digest.Write([]byte{0})
		digest.Write(data)
	}
	if got := hex.EncodeToString(digest.Sum(nil))[:32]; got != GeneratorFingerprint || got == legacyInstallerFingerprint {
		t.Fatalf("rollback installer fingerprint %s; update GeneratorFingerprint (was %s)", got, GeneratorFingerprint)
	}
}
