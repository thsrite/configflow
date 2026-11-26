package deploy

import (
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
)

func generatedFailure(err error, content, service string) *PreparationError {
	var validation *oxidns.ValidationError
	var issues []oxidns.Issue
	if errors.As(err, &validation) {
		issues = validation.Issues
	}
	at := Position{}
	if len(issues) > 0 {
		for _, issue := range issues {
			if issue.Severity != "" && issue.Severity != "error" {
				continue
			}
			n, e := yamlx.Parse([]byte(content))
			if e != nil {
				break
			}
			for _, part := range issue.Path {
				if part.IsIndex {
					n = yamlx.Resolve(n)
					if n != nil && n.Kind == yaml.SequenceNode && part.Index >= 0 && part.Index < len(n.Content) {
						n = n.Content[part.Index]
					} else {
						n = nil
					}
				} else {
					n = yamlx.Get(n, part.Key)
				}
				if n == nil {
					break
				}
			}
			if n != nil {
				line, col := n.Line, n.Column
				at = Position{Line: &line, Column: &col}
			}
			break
		}
	}
	message := err.Error()
	var syntax *yamlx.SyntaxError
	var cause error
	if errors.As(err, &syntax) {
		message = msgGeneratedYAML
		cause = err
	}
	p := PreparationContext(SourceError(message, content, at, cause, ""), "", msgGeneratedConfig, fmt.Sprintf(msgConfigKind, service))
	p.Issues = issues
	return p
}
