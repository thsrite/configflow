// Package installer renders the installer scripts and Docker snippets of
// ConfigFlow's Agents (port of backend/agents/install_output.py,
// install_script.py, go_install_script.py, core_install_script.py,
// gateway_install_script.py) and keeps the short-lived install links
// (install_command_store.py).
//
// Renderers preserve shell commands and effects. JSON/YAML configuration and
// archive payloads are compared structurally; formatting and compression use
// native Go libraries. Embedded shell templates remain synchronized with backend.
package installer

// ValidationError is an installer input refusal exposed as HTTP 400.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }
func valueError(message string) error    { return &ValidationError{Message: message} }

// RenderError rejects a malformed internal renderer or stored capability shape.
// It carries a native diagnostic, never an interpreter exception or credential.
type RenderError struct{ Message string }

func (e *RenderError) Error() string { return e.Message }
func typeError(message string) error { return &RenderError{Message: message} }
