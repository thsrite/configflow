// Package installer renders the installer scripts and Docker snippets of
// ConfigFlow's Agents (port of backend/agents/install_output.py,
// install_script.py, go_install_script.py, core_install_script.py,
// gateway_install_script.py) and keeps the short-lived install links
// (install_command_store.py).
//
// Users run the rendered scripts as root, so every renderer must produce the
// same bytes as the Python code for the same inputs. The shell templates are
// embedded from scripts/, a copy of backend/agents/scripts/ that
// TestScriptsMatchBackend keeps identical.
//
// Errors a Python generator raises as ValueError are *ValueError here; the
// routes answer them with their text, as Flask does with str(exc).
package installer

// ValueError is a ValueError raised by a Python generator; Error() is str(exc).
type ValueError struct{ Message string }

func (e *ValueError) Error() string { return e.Message }

func valueError(message string) error { return &ValueError{Message: message} }
