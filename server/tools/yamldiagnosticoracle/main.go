// Command yamldiagnosticoracle freezes provider/A11 diagnostics at a named
// pre-migration revision. It calls the real public Go boundary and never builds
// replacement parser values or modifies a business function.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"

	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/provider"
)

func readFile(path string, compressed bool) (any, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	if compressed {
		z, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, "", err
		}
		data, err = io.ReadAll(z)
		_ = z.Close()
		if err != nil {
			return nil, "", err
		}
	}
	value, err := jsonx.Parse(data)
	return value, hex.EncodeToString(sum[:]), err
}

func run() error {
	base := flag.String("base", "", "frozen pre-migration commit")
	corpus := flag.String("corpus", "internal/provider/testdata/corpus.json.gz", "real caller corpus")
	controls := flag.String("controls", "internal/provider/testdata/n73_diagnostic_inputs.json", "position boundary inputs")
	flag.Parse()
	if *base == "" {
		return fmt.Errorf("a frozen baseline commit is required")
	}
	value, corpusSHA, err := readFile(*corpus, true)
	if err != nil {
		return err
	}
	boundary, controlsSHA, err := readFile(*controls, false)
	if err != nil {
		return err
	}
	inputs := jsonx.Obj()
	for index, item := range value.([]any) {
		row := item.(*jsonx.Object)
		if row.GetString("kind") == "parse" {
			inputs.Set(fmt.Sprintf("corpus/%d", index), row.GetObject("input"))
		}
	}
	for name, input := range boundary.(*jsonx.Object).All() {
		inputs.Set("control/"+name, input)
	}
	restore := provider.SetLocations(deploy.Locations{})
	defer restore()
	cases := jsonx.Obj()
	for name, value := range inputs.All() {
		input := value.(*jsonx.Object)
		_, err := provider.ParseProviderProxies(input.GetString("text"), input.Value("name"), input.Value("path"))
		message := ""
		if err != nil {
			message = err.Error()
		}
		cases.Set(name, jsonx.Obj("input", input, "rejected", err != nil, "message", message))
	}
	data, err := jsonx.Sorted(jsonx.Obj("base", *base, "corpus_sha256", corpusSHA, "controls_sha256", controlsSHA, "cases", cases))
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
