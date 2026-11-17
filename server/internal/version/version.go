// Package version is the port of backend/version.py.
package version

// Version is the release the binary was built as. The image build sets it
// (Python rewrote `__version__ = "dev"` in version.py with sed):
//
//	go build -ldflags "-X configflow/internal/version.Version=${VERSION}"
//
// A local build reports "dev".
var Version = "dev"

// Information is the public build metadata.
type Information struct {
	Version string `json:"version"`
}

// Info returns the current release metadata.
func Info() Information { return Information{Version: Version} }
