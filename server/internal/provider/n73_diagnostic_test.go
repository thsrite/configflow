package provider_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/provider"
	"configflow/internal/yamlx/yamlxtest"
)

func TestN73NativeDiagnosticInterface(t *testing.T) {
	fixture := yamlxtest.Fixture(t, "testdata/n73_diagnostic_baseline.json.gz")
	if fixture.GetString("base") != "b6fe812a" {
		t.Fatal("diagnostic reference must come from the frozen pre-interface revision")
	}
	for name, field := range map[string]string{
		"testdata/corpus.json.gz":             "corpus_sha256",
		"testdata/n73_diagnostic_inputs.json": "controls_sha256",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != fixture.GetString(field) {
			t.Fatalf("diagnostic source changed: %s", name)
		}
	}
	t.Cleanup(provider.SetLocations(deploy.Locations{}))
	cases := fixture.GetObject("cases")
	if cases.Len() != 235 {
		t.Fatalf("diagnostic reference has %d cases, want235", cases.Len())
	}
	rejected := 0
	for name, raw := range cases.All() {
		record := raw.(*jsonx.Object)
		input := record.GetObject("input")
		_, err := provider.ParseProviderProxies(input.GetString("text"), input.Value("name"), input.Value("path"))
		message := ""
		if err != nil {
			rejected++
			message = err.Error()
		}
		if (err != nil) != record.GetBool("rejected") || message != record.GetString("message") {
			t.Errorf("diagnostic/position changed for %s: got %q want %q", name, message, record.GetString("message"))
		}
	}
	t.Logf("sources=%d rejected=%d position changes=0 unknown=0 (complete visible text compared)", cases.Len(), rejected)
}
