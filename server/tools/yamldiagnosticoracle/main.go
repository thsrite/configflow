// Command yamldiagnosticoracle freezes provider/A11 diagnostics at a named
// pre-migration revision. It calls the real public Go boundary and never builds
// replacement parser values or modifies a business function.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"configflow/internal/deploy"
	"configflow/internal/logx"
	"configflow/internal/provider"
)

type diagnosticInput struct {
	Text string `json:"text"`
	Name any    `json:"name,omitempty"`
	Path any    `json:"path,omitempty"`
}

func readJSON(path string, compressed bool, target any) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	if compressed {
		z, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		data, err = io.ReadAll(z)
		_ = z.Close()
		if err != nil {
			return "", err
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum[:]), nil
}

func labelOf(v any) string {
	s, _ := v.(string)
	return s
}

func run() error {
	base := flag.String("base", "", "frozen pre-migration commit")
	corpus := flag.String("corpus", "internal/provider/testdata/corpus.json.gz", "real caller corpus")
	controls := flag.String("controls", "internal/provider/testdata/n73_diagnostic_inputs.json", "position boundary inputs")
	flag.Parse()
	if *base == "" {
		return fmt.Errorf("a frozen baseline commit is required")
	}
	var rows []struct {
		Kind  string          `json:"kind"`
		Input json.RawMessage `json:"input"`
	}
	corpusSHA, err := readJSON(*corpus, true, &rows)
	if err != nil {
		return err
	}
	var boundary map[string]json.RawMessage
	controlsSHA, err := readJSON(*controls, false, &boundary)
	if err != nil {
		return err
	}
	inputs := map[string]json.RawMessage{}
	for index, row := range rows {
		if row.Kind == "parse" {
			inputs[fmt.Sprintf("corpus/%d", index)] = row.Input
		}
	}
	for name, input := range boundary {
		inputs["control/"+name] = input
	}
	restore := provider.SetLocations(deploy.Locations{})
	defer restore()
	type caseRecord struct {
		Input    json.RawMessage `json:"input"`
		Message  string          `json:"message"`
		Rejected bool            `json:"rejected"`
	}
	cases := make(map[string]caseRecord, len(inputs))
	for name, raw := range inputs {
		var input diagnosticInput
		if err := json.Unmarshal(raw, &input); err != nil {
			return err
		}
		_, err := provider.ParseNodes(input.Text, labelOf(input.Name), labelOf(input.Path))
		message := ""
		if err != nil {
			message = err.Error()
		}
		cases[name] = caseRecord{Input: raw, Rejected: err != nil, Message: message}
	}
	data, err := json.Marshal(struct {
		Base        string                `json:"base"`
		Cases       map[string]caseRecord `json:"cases"`
		ControlsSHA string                `json:"controls_sha256"`
		CorpusSHA   string                `json:"corpus_sha256"`
	}{*base, cases, controlsSHA, corpusSHA})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

func main() {
	if err := run(); err != nil {
		logx.Get("tools.yamldiagnosticoracle").Errorf("record diagnostic baseline: %v", err)
		os.Exit(1)
	}
}
