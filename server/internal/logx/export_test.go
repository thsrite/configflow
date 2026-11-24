package logx

import (
	"os"
	"strings"
)

// IsDebugEnabled is is_debug_enabled: LOG_LEVEL is DEBUG.
func IsDebugEnabled() bool {
	name := os.Getenv("LOG_LEVEL")
	if name == "" {
		name = "INFO"
	}
	return strings.ToUpper(name) == "DEBUG"
}
