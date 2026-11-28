package logs

import (
	"fmt"
	"strings"

	"configflow/internal/httpx"
	"configflow/internal/pathx"
	"configflow/internal/rules"
)

// getLogsBefore is the original full-file handler for differential tests and benchmarks.
func (h *Handler) getLogsBefore(c *httpx.Ctx) (*httpx.Response, error) {
	logPath, err := h.logFilePath()
	if err != nil {
		return errorResponse(err, true), nil
	}
	lines := c.ArgInt("lines", 100)
	search := c.ArgDefault("search", "")
	levelFilter := strings.ToUpper(c.ArgDefault("level", ""))
	lines = min(lines, 10000)
	present, err := pathx.Exists(logPath)
	if err != nil {
		return errorResponse(err, true), nil
	}
	if !present {
		return httpx.JSON(tailResponse{Success: true, Logs: []string{}, Message: fmt.Sprintf(msgMissingPath, logPath)}), nil
	}
	file, err := h.openLogFile(logPath)
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
