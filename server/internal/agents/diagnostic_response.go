package agents

import (
	"encoding/json"
	"strings"

	"configflow/internal/model"
	"configflow/internal/redact"
)

// ResponseFailureModel keeps bounded string diagnostics from an Agent reply.
// Exact keys and shared opaque fields prevent case aliases, duplicates or R5
// projections from manufacturing a diagnostic. Plain text is retained; HTML is not.
func ResponseFailureModel(response *Response, fallback string, secrets redact.Secrets) (*model.Deployment, error) {
	if response == nil {
		return nil, ErrInvalidAgentReply
	}
	var fields model.Fields
	parsed := json.Unmarshal(response.jsonData(), &fields) == nil
	result := new(model.Deployment)
	edit := editDeployment(result).flag("success", false)
	if parsed {
		for _, key := range []string{"message", "error", "error_details", "rollback_error", "rollback_error_details", "failed_stage"} {
			raw, present := fields.Get(key)
			if !present {
				continue
			}
			var value *string
			if json.Unmarshal(raw, &value) == nil && value != nil {
				edit.text(key, redact.DiagnosticText(*value, secrets, 0))
			}
		}
	}
	if originalDeploymentText(result, "message") == "" {
		message := originalDeploymentText(result, "error")
		if message == "" {
			message = redact.DiagnosticText(fallback, secrets, 0)
		}
		edit.text("message", message)
	}
	if !parsed {
		contentType, _, _ := strings.Cut(response.contentType(), ";")
		if contentType == "text/plain" {
			if detail := redact.DiagnosticText(response.Text(), secrets, 0); detail != "" {
				edit.text("error_details", detail)
			}
		}
	}
	if response.StatusCode >= 400 {
		edit.code(response.StatusCode)
	}
	return edit.result()
}
