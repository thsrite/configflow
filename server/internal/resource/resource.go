// Package resource is the port of backend/common/resource.py: locating the
// files shipped next to the program (config_template.json, VERSION, ...).
//
// The Python code found them relative to its own source tree or to the
// PyInstaller bundle directory. The Go server resolves them under one root
// directory instead: the image's /app (where `COPY backend/ /app/backend/`
// puts them), the repository root in a development checkout, or whatever
// CONFIGFLOW_ROOT names. Embedded resources (embed.FS) are the business of the
// package that owns them.
package resource

import (
	"os"
	"strings"
)

// Locator resolves resource paths under Root.
type Locator struct {
	// Root plays the part of the "project root" (the directory holding backend/).
	Root string
}

// New returns a locator for root.
func New(root string) Locator { return Locator{Root: root} }

// FromEnv picks the root like the deployment does: $CONFIGFLOW_ROOT if set,
// else /app when /app/backend exists (the image), else the working directory.
func FromEnv() Locator {
	if root := os.Getenv("CONFIGFLOW_ROOT"); root != "" {
		return New(root)
	}
	if info, err := os.Stat("/app/backend"); err == nil && info.IsDir() {
		return New("/app")
	}
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return New(wd)
}

// join is os.path.join(base, rel): an absolute rel replaces base, nothing is cleaned.
func join(base, rel string) string {
	if strings.HasPrefix(rel, "/") {
		return rel
	}
	if base == "" || strings.HasSuffix(base, "/") {
		return base + rel
	}
	return base + "/" + rel
}

// Path is get_resource_path: relativePath (such as "backend/VERSION") under the root.
func (l Locator) Path(relativePath string) string { return join(l.Root, relativePath) }

// Backend is get_backend_resource: filename inside the backend directory
// (such as "config_template.json").
func (l Locator) Backend(filename string) string { return join(join(l.Root, "backend"), filename) }

// Exists is resource_exists: the path exists and is a regular file.
func (l Locator) Exists(relativePath string) bool {
	info, err := os.Stat(l.Path(relativePath))
	return err == nil && info.Mode().IsRegular()
}
