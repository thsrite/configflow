package provider_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"testing"

	"configflow/internal/deploy"
	"configflow/internal/provider"
)

func TestN73NativeDiagnosticInterface(t *testing.T) {
	f, err := os.Open("testdata/n73_diagnostic_baseline.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	raw, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Base           string `json:"base"`
		CorpusSHA256   string `json:"corpus_sha256"`
		ControlsSHA256 string `json:"controls_sha256"`
		Cases          map[string]struct {
			Input struct {
				Text string `json:"text"`
				Name any    `json:"name"`
				Path any    `json:"path"`
			} `json:"input"`
			Rejected bool   `json:"rejected"`
			Message  string `json:"message"`
		} `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Base != "b6fe812a" {
		t.Fatal("diagnostic reference must come from the frozen pre-interface revision")
	}
	for name, expected := range map[string]string{
		"testdata/corpus.json.gz":             fixture.CorpusSHA256,
		"testdata/n73_diagnostic_inputs.json": fixture.ControlsSHA256,
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != expected {
			t.Fatalf("diagnostic source changed: %s", name)
		}
	}
	pv := provider.New(provider.Options{Locations: deploy.Locations{}})
	if len(fixture.Cases) != 235 {
		t.Fatalf("diagnostic reference has %d cases, want 235", len(fixture.Cases))
	}
	rejected := 0
	for name, record := range fixture.Cases {
		inName, inPath := "", ""
		if record.Input.Name != nil && record.Input.Name != false && fmt.Sprint(record.Input.Name) != "0" {
			inName = fmt.Sprint(record.Input.Name)
		}
		if record.Input.Path != nil && record.Input.Path != false && fmt.Sprint(record.Input.Path) != "0" {
			inPath = fmt.Sprint(record.Input.Path)
		}
		_, err := pv.ParseNodes(record.Input.Text, inName, inPath)
		message := ""
		if err != nil {
			rejected++
			message = err.Error()
		}
		if (err != nil) != record.Rejected || message != record.Message {
			t.Errorf("diagnostic/position changed for %s: got %q want %q", name, message, record.Message)
		}
	}
	t.Logf("sources=%d rejected=%d position changes=0 unknown=0 (complete visible text compared)", len(fixture.Cases), rejected)
}
