// Package logs holds the routes of backend/routes/logs.py (blueprint
// "logs", prefix /api/logs): tail, info and clear of the application log.
// The blueprint has no require_auth; the global gate protects it.
package logs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/rules"
)

// Register adds the logs blueprint.
func Register(r *httpx.Router) {
	r.GET("/api/logs/tail", "logs.get_logs", getLogs)
	r.GET("/api/logs/info", "logs.get_log_info", getLogInfo)
	r.POST("/api/logs/clear", "logs.clear_logs", clearLogs)
}

// LogFilePath is get_log_file_path(); tests replace it.
var LogFilePath = getLogFilePath

// openFile is open(); tests replace it.
var openFile = func(path string) (io.ReadCloser, error) { return os.Open(path) }

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func getLogFilePath() (string, error) {
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}
	if _, err := os.Stat(dataDir); err != nil {
		dataDir = "."
	}
	defaultLogFile := pathJoin(dataDir, "app.log")
	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = defaultLogFile
	}
	logPath := pathStr(logFile)
	present, err := pathExists(logPath)
	if err != nil {
		return "", err
	}
	if !present {
		for _, candidate := range []string{pathJoin(dataDir, "app.log"), "./app.log", "data/app.log",
			"logs/app.log", "/var/log/configflow/app.log", "app.log"} {
			present, err := pathExists(candidate)
			if err != nil {
				return "", err
			}
			if present {
				logPath = pathStr(candidate)
				break
			}
		}
	}
	return logPath, nil
}

// pathStr is str(Path(p)): redundant slashes and "." segments removed.
func pathStr(p string) string {
	if p == "" {
		return "."
	}
	absolute := strings.HasPrefix(p, "/")
	var parts []string
	for _, part := range strings.Split(p, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	joined := strings.Join(parts, "/")
	if absolute {
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			return "//" + joined
		}
		return "/" + joined
	}
	if joined == "" {
		return "."
	}
	return joined
}

func pathJoin(dir, name string) string { return pathStr(dir + "/" + name) }

func errorResponse(err error, withLogs bool) *httpx.Response {
	if withLogs {
		return httpx.JSONStatus(500, struct {
			Success bool     `json:"success"`
			Error   string   `json:"error"`
			Logs    []string `json:"logs"`
			Total   int      `json:"total_lines"`
		}{false, err.Error(), []string{}, 0})
	}
	return httpx.JSONStatus(500, struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}{false, err.Error()})
}

type tailResponse struct {
	Success  bool     `json:"success"`
	Logs     []string `json:"logs"`
	Total    int      `json:"total_lines"`
	Filtered *int     `json:"filtered_lines,omitempty"`
	File     string   `json:"log_file,omitempty"`
	Message  string   `json:"message,omitempty"`
}

func getLogs(c *httpx.Ctx) (*httpx.Response, error) {
	logPath, err := LogFilePath()
	if err != nil {
		return errorResponse(err, true), nil
	}
	lines := c.ArgInt("lines", 100)
	search := c.ArgDefault("search", "")
	levelFilter := strings.ToUpper(c.ArgDefault("level", ""))
	lines = min(lines, 10000)
	present, err := pathExists(logPath)
	if err != nil {
		return errorResponse(err, true), nil
	}
	if !present {
		return httpx.JSON(tailResponse{Success: true, Logs: []string{}, Message: fmt.Sprintf(msgMissingPath, logPath)}), nil
	}
	file, err := openFile(logPath)
	if err != nil {
		return errorResponse(err, true), nil
	}
	var logLines []string
	total := 0
	err = readLines(file, func(line string) {
		total++
		logLines = append(logLines, line)
		if lines > 0 && int64(len(logLines)) > lines {
			logLines = logLines[1:]
		}
	})
	file.Close()
	if err != nil {
		return errorResponse(err, true), nil
	}
	var level *rules.PyRegex
	if levelFilter != "" {
		level, err = rules.CompilePyRegex(`\b`+levelFilter+`\b`, true)
		if err != nil {
			return errorResponse(err, true), nil
		}
	}
	searchLower := pyLower(search)
	filtered := []string{}
	for _, line := range logLines {
		if search != "" && !strings.Contains(pyLower(line), searchLower) {
			continue
		}
		if level != nil && !level.Search(line) {
			continue
		}
		filtered = append(filtered, strings.TrimRight(line, "\n"))
	}
	return httpx.JSON(tailResponse{Success: true, Logs: filtered, Total: total, Filtered: new(len(filtered)), File: logPath}), nil
}

// readLines iterates a text file like Python's text mode with
// encoding="utf-8", errors="ignore" and universal newlines: invalid bytes
// are dropped, "\r\n" and "\r" become "\n", each line keeps its "\n".
func readLines(r io.Reader, line func(string)) error {
	reader := bufio.NewReaderSize(r, 64*1024)
	var current bytes.Buffer
	pendingCR := false
	flush := func() {
		line(dropInvalidUTF8(current.Bytes()))
		current.Reset()
	}
	for {
		b, err := reader.ReadByte()
		if err == io.EOF {
			if pendingCR {
				current.WriteByte('\n')
				flush()
			} else if current.Len() > 0 {
				flush()
			}
			return nil
		}
		if err != nil {
			return err
		}
		if pendingCR {
			pendingCR = false
			current.WriteByte('\n')
			flush()
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\r':
			pendingCR = true
		case '\n':
			current.WriteByte('\n')
			flush()
		default:
			current.WriteByte(b)
		}
	}
}

func dropInvalidUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	var out strings.Builder
	for len(b) > 0 {
		r, size := utf8.DecodeRune(b)
		if r != utf8.RuneError || size > 1 {
			out.Write(b[:size])
		}
		b = b[size:]
	}
	return out.String()
}

// pyLower is str.lower() closely enough for log search (Go's simple case
// mapping; Python's special full mappings such as U+0130 differ).
func pyLower(s string) string { return strings.ToLower(s) }

func getLogInfo(c *httpx.Ctx) (*httpx.Response, error) {
	logPath, err := LogFilePath()
	if err != nil {
		return errorResponse(err, false), nil
	}
	info, err := os.Stat(logPath)
	if errors.Is(err, fs.ErrNotExist) {
		return httpx.JSON(struct {
			Success bool   `json:"success"`
			Exists  bool   `json:"exists"`
			Path    string `json:"path"`
		}{true, false, logPath}), nil
	}
	if err != nil {
		return errorResponse(err, false), nil
	}
	size := info.Size()
	modified, created := fileTimes(info)
	return httpx.JSON(struct {
		Success  bool    `json:"success"`
		Exists   bool    `json:"exists"`
		Path     string  `json:"path"`
		Size     int64   `json:"size"`
		SizeMB   float64 `json:"size_mb"`
		Modified string  `json:"modified"`
		Created  string  `json:"created"`
	}{true, true, logPath, size, pyRound2(float64(size) / (1024 * 1024)), isoformat(modified), isoformat(created)}), nil
}

// pyRound2 is round(x, 2): correctly rounded, ties to even.
func pyRound2(x float64) float64 {
	v, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'f', 2, 64), 64)
	return v
}

// isoformat is datetime.fromtimestamp(t).isoformat(): local time, with
// microseconds only when they are not zero.
func isoformat(t time.Time) string {
	t = t.Local()
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05")
	}
	return t.Format("2006-01-02T15:04:05.000000")
}

func clearLogs(c *httpx.Ctx) (*httpx.Response, error) {
	return clearLog(c.Context())
}

func clearLog(ctx context.Context) (*httpx.Response, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logPath, err := LogFilePath()
	if err != nil {
		return errorResponse(err, false), nil
	}
	present, err := pathExists(logPath)
	if err != nil {
		return errorResponse(err, false), nil
	}
	if !present {
		return httpx.Message(200, true, msgMissing), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(logPath, nil, 0o666); err != nil {
		return errorResponse(err, false), nil
	}
	return httpx.Message(200, true, msgCleared), nil
}
