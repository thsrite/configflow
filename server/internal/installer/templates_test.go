package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"configflow/internal/installer/installertest"
)

// TestRollbackModuleMatchesApprovedDelta keeps the embedded L1 D3 rollback
// helper equal to its approved source delta.
func TestRollbackModuleMatchesApprovedDelta(t *testing.T) {
	wantModule, err := installertest.RollbackModule()
	if err != nil || agentInstallRollback != wantModule {
		t.Fatalf("L1 D3 rollback helper differs from its approved source delta: %v", err)
	}
}

// TestGeneratorFingerprintMatchesReleaseInputs derives GeneratorFingerprint
// from the frozen legacy fingerprint and the embedded rollback-era scripts.
func TestGeneratorFingerprintMatchesReleaseInputs(t *testing.T) {
	if legacyInstallerFingerprint != installertest.LegacyGeneratorFingerprint {
		t.Fatal("legacy fingerprint reference drift")
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
