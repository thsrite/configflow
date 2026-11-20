// Command yamlreaderoracle records native loader values before Python consumes them.
// Only Parse, the production key projector and the physical JSON transition are
// invoked; no Go consumer business function participates in expected results.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"configflow/internal/logx"
	"configflow/internal/yamlx"
	"configflow/internal/yamlx/yamlxtest"
)

func run() error {
	d, e := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	for {
		var request struct {
			Source string `json:"source"`
			Mode   string `json:"mode"`
		}
		if err := d.Decode(&request); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		answer := map[string]any{"source": request.Source, "mode": request.Mode}
		category, comparisonErr := yamlxtest.Classify(request.Source)
		root, err := yamlx.Parse([]byte(request.Source))
		if err == nil {
			answer["parse_nodes"] = yamlxtest.ParseAudit(root)
			for _, node := range yamlxtest.ParseAudit(root) {
				if node.Tag == "!!float" && (node.Value == ".inf" || node.Value == "-.inf" || node.Value == ".nan") {
					category = "nonfinite-scalar-text"
				}
			}
			mode := yamlx.RejectNonStringKeys
			if request.Mode == "convert-proxy" {
				mode = yamlx.StringifyScalarKeys
			}
			err = yamlx.ProjectJSONKeys(root, mode)
			if errors.Is(err, yamlx.ErrType) {
				category = yamlxtest.Policy
				comparisonErr = nil
			}
		}
		if comparisonErr != nil {
			answer["unclassified"] = comparisonErr.Error()
		}
		answer["class"] = category
		if err == nil {
			var value any
			value, err = yamlxtest.Value(root)
			if err == nil {
				answer["values"] = yamlxtest.ValueGraph(value)
			}
		}
		if err != nil {
			answer["error"] = err.Error()
		}
		if err := e.Encode(answer); err != nil {
			return err
		}
	}
}
func main() {
	if err := run(); err != nil {
		logx.Get("yamlreaderoracle").Error(fmt.Sprintf("reader recording failed: %v", err))
		os.Exit(1)
	}
}
