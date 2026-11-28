package agents

// GET /api/agents/download/<filename> (routes/agents.py
// download_agent_binary, module A10): the Go Agent executables offered to
// installers. Public (no panel login; listed in the auth gate).

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"configflow/internal/httpx"
	"configflow/internal/pathx"
	"configflow/internal/resource"
)

// downloadableAgentBinaries is allowed_files.
var downloadableAgentBinaries = []string{
	"configflow-agent-linux-amd64",
	"configflow-agent-linux-arm64",
	"configflow-agent-linux-armv7",
}

type downloadsHandler struct{}

func (h *downloadsHandler) register(r *httpx.Router) {
	r.GET("/api/agents/download/<filename>", "agents.download_agent_binary", h.downloadAgentBinary)
}

// agentBinaryDirectory is the directory download_agent_binary serves:
// os.getenv('AGENTS_STATIC_DIR', '/opt/configflow/static/agents') when
// os.path.exists() (so an empty value falls back), else backend/static/agents.
func agentBinaryDirectory() string {
	directory, set := os.LookupEnv("AGENTS_STATIC_DIR")
	if !set {
		directory = "/opt/configflow/static/agents"
	}
	if !pathx.Accessible(directory) {
		directory = resource.FromEnv().Backend("static/agents")
	}
	return directory
}

// pyJoin is os.path.join(a, b) for a relative b.
func pyJoin(a, b string) string {
	if a == "" || strings.HasSuffix(a, "/") {
		return a + b
	}
	return a + "/" + b
}

// pyOSErrorText is str(OSError) for a failed file operation on path.
func pyOSErrorText(err error, path string) string { return err.Error() }

func (h *downloadsHandler) downloadAgentBinary(c *httpx.Ctx) (*httpx.Response, error) {
	filename := c.Param("filename")
	directory := agentBinaryDirectory()
	allowed := false
	for _, name := range downloadableAgentBinaries {
		allowed = allowed || name == filename
	}
	if !allowed {
		return failure(404, "Invalid filename"), nil
	}
	path := pyJoin(directory, filename)
	if !pathx.Accessible(path) {
		logger.Errorf("Agent binary not found: %s", path)
		return failure(404, "File not found"), nil
	}
	// flask.send_file joins a relative path with the application root
	// (backend/), then stats and opens it; its OSErrors reach the view's
	// `except Exception` (500 with str(e)).
	served := path
	if !filepath.IsAbs(served) {
		served = pyJoin(resource.FromEnv().Path("backend"), served)
	}
	fail := func(err error) (*httpx.Response, error) {
		message := pyOSErrorText(err, served)
		logger.Errorf("Error downloading agent binary: %s", message)
		return failure(500, message), nil
	}
	info, err := os.Stat(served)
	if err != nil {
		return fail(err)
	}
	if info.IsDir() {
		return fail(&fs.PathError{Op: "open", Path: served, Err: syscall.EISDIR})
	}
	stream, err := os.Open(served)
	if err != nil {
		return fail(err)
	}
	stream.Close()
	response, err := httpx.File(httpx.FileOptions{
		Path: served, MimeType: "application/octet-stream", AsAttachment: true, DownloadName: filename,
		DiscardAfterSend: true,
	})
	if err != nil {
		return fail(err)
	}
	// Range, If-Range and conditional headers are handled by http.ServeContent.
	return response, nil
}
