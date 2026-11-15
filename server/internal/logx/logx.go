// Package logx is the port of backend/utils/logger.py and of the logging
// setup at the top of backend/app.py: the same line format, level handling,
// log file and rotation, so the log page and the Agents' diagnostics keep
// reading what they always read.
//
//	2026-11-14 12:34:56,789 - backend.routes.agents - INFO - message
//
// Use the Python module names as logger names (Get("backend.routes.agents")).
package logx

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"configflow/internal/jsonx"
)

// Level is a Python logging level number.
type Level int

const (
	Debug    Level = 10
	Info     Level = 20
	Warning  Level = 30
	Error    Level = 40
	Critical Level = 50
)

// String is logging.getLevelName.
func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warning:
		return "WARNING"
	case Error:
		return "ERROR"
	case Critical:
		return "CRITICAL"
	}
	return fmt.Sprintf("Level %d", int(l))
}

// ParseLevel maps a level name like LOG_LEVEL to a Level: case-insensitive,
// anything but the five standard names (WARN included) is INFO.
func ParseLevel(name string) Level {
	switch strings.ToUpper(name) {
	case "DEBUG":
		return Debug
	case "WARNING":
		return Warning
	case "ERROR":
		return Error
	case "CRITICAL":
		return Critical
	}
	return Info
}

// LevelFromEnv is the level named by LOG_LEVEL (default INFO).
func LevelFromEnv() Level {
	name := os.Getenv("LOG_LEVEL")
	if name == "" {
		name = "INFO"
	}
	return ParseLevel(name)
}

// IsDebugEnabled is is_debug_enabled: LOG_LEVEL is DEBUG.
func IsDebugEnabled() bool {
	name := os.Getenv("LOG_LEVEL")
	if name == "" {
		name = "INFO"
	}
	return strings.ToUpper(name) == "DEBUG"
}

// Config is the logging setup of app.py.
type Config struct {
	// Level is the threshold; LevelFromEnv() in production.
	Level Level
	// File is the rotating log file; empty disables it. The server uses
	// $LOG_FILE or DATA_DIR/app.log (see DefaultFile).
	File string
	// Console receives every line too (os.Stderr in production); nil disables it.
	Console io.Writer
	// MaxBytes and Backups configure the rotation; zero means 10 MiB and 5.
	MaxBytes int64
	Backups  int
	// Clock is for tests; nil means time.Now.
	Clock func() time.Time
}

// DefaultFile is the log file path of app.py: $LOG_FILE, else dataDir/app.log.
func DefaultFile(dataDir string) string {
	if file := os.Getenv("LOG_FILE"); file != "" {
		return file
	}
	return filepath.Join(dataDir, "app.log")
}

// Root is the process-wide logging state (Python's root logger and its handlers).
type Root struct {
	mu      sync.Mutex
	level   Level
	console io.Writer
	file    *rotatingFile
	clock   func() time.Time
}

var (
	rootMu  sync.Mutex
	current = newRoot(Config{Level: LevelFromEnv(), Console: os.Stderr})
)

func newRoot(c Config) *Root {
	r := &Root{level: c.Level, console: c.Console, clock: c.Clock}
	if r.clock == nil {
		r.clock = time.Now
	}
	return r
}

// Setup installs the handlers of app.py (rotating file plus console) and
// returns the root; later Get calls log through it. The data directory of the
// file is created.
func Setup(c Config) (*Root, error) {
	r := newRoot(c)
	if c.File != "" {
		if err := os.MkdirAll(filepath.Dir(c.File), 0o777); err != nil {
			return nil, err
		}
		maxBytes, backups := c.MaxBytes, c.Backups
		if maxBytes == 0 {
			maxBytes = 10 * 1024 * 1024
		}
		if backups == 0 {
			backups = 5
		}
		file, err := openRotating(c.File, maxBytes, backups)
		if err != nil {
			return nil, err
		}
		r.file = file
	}
	rootMu.Lock()
	old := current
	current = r
	rootMu.Unlock()
	if old != nil {
		old.Close()
	}
	return r, nil
}

// Close flushes and closes the log file.
func (r *Root) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		err := r.file.close()
		r.file = nil
		return err
	}
	return nil
}

// SetLevel changes the threshold.
func (r *Root) SetLevel(level Level) {
	r.mu.Lock()
	r.level = level
	r.mu.Unlock()
}

func (r *Root) log(name string, level Level, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if level < r.level {
		return
	}
	now := r.clock()
	line := FormatLine(now, name, level, message) + "\n"
	if r.file != nil {
		r.file.write(line)
	}
	if r.console != nil {
		_, _ = io.WriteString(r.console, line)
	}
}

// FormatLine renders '%(asctime)s - %(name)s - %(levelname)s - %(message)s':
// the local time with comma milliseconds, as logging.Formatter writes it.
func FormatLine(t time.Time, name string, level Level, message string) string {
	return fmt.Sprintf("%s,%03d - %s - %s - %s", t.Format("2006-01-02 15:04:05"), t.Nanosecond()/1_000_000, name, level, message)
}

// Logger is a named logger (logging.getLogger(name)).
type Logger struct{ name string }

// Get is get_logger(name): a logger that writes through the current Root at
// the level the root was set up with.
func Get(name string) *Logger { return &Logger{name: name} }

// Name is the logger name.
func (l *Logger) Name() string { return l.name }

func (l *Logger) root() *Root {
	rootMu.Lock()
	defer rootMu.Unlock()
	return current
}

// Log writes message at level.
func (l *Logger) Log(level Level, message string) { l.root().log(l.name, level, message) }

// Enabled is logger.isEnabledFor(level).
func (l *Logger) Enabled(level Level) bool {
	r := l.root()
	r.mu.Lock()
	defer r.mu.Unlock()
	return level >= r.level
}

func (l *Logger) Debug(message string)    { l.Log(Debug, message) }
func (l *Logger) Info(message string)     { l.Log(Info, message) }
func (l *Logger) Warning(message string)  { l.Log(Warning, message) }
func (l *Logger) Error(message string)    { l.Log(Error, message) }
func (l *Logger) Critical(message string) { l.Log(Critical, message) }

// The *f methods format like fmt.Sprintf; the arguments are only formatted
// when the level is enabled.
func (l *Logger) Debugf(format string, args ...any)    { l.logf(Debug, format, args) }
func (l *Logger) Infof(format string, args ...any)     { l.logf(Info, format, args) }
func (l *Logger) Warningf(format string, args ...any)  { l.logf(Warning, format, args) }
func (l *Logger) Errorf(format string, args ...any)    { l.logf(Error, format, args) }
func (l *Logger) Criticalf(format string, args ...any) { l.logf(Critical, format, args) }

// Exception is logger.exception: an ERROR line with the error text in place
// of Python's traceback.
func (l *Logger) Exception(message string, err error) {
	if err != nil {
		message += "\n" + err.Error()
	}
	l.Log(Error, message)
}

func (l *Logger) logf(level Level, format string, args []any) {
	if l.Enabled(level) {
		l.Log(level, fmt.Sprintf(format, args...))
	}
}

// DebugLog is debug_log (deprecated in Python): a DEBUG line from "debug_log".
func DebugLog(message string) { Get("debug_log").Debug(message) }

// LogText is log_text(value, limit): user-supplied text on one bounded log
// line. Characters Python's str.isprintable() rejects (line breaks and other
// controls included) become "?", so a value cannot forge log entries; text
// longer than limit characters is cut and gets an ellipsis. Use limit 120
// where Python used the default.
func LogText(value any, limit int) string {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case error:
		text = v.Error()
	case fmt.Stringer:
		text = v.String()
	default:
		text = jsonx.Str(value)
	}
	var b strings.Builder
	count := 0
	truncated := false
	for _, r := range text {
		if count == limit {
			truncated = true
			break
		}
		if IsPrintable(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('?')
		}
		count++
	}
	if truncated {
		b.WriteString("…")
	}
	return b.String()
}

// IsPrintable is str.isprintable() for one character (Python 3.11, Unicode 14).
func IsPrintable(r rune) bool { return jsonx.IsPrintable(r) }

// ClientIP is client_ip(): the request source for operation logs, X-Real-IP
// (set by nginx) or else the socket address, through LogText with limit 64.
// A nil request, Python's "no request context", gives "-".
func ClientIP(r *http.Request) string {
	if r == nil {
		return "-"
	}
	// WSGI hands header values over as latin-1 decoded text (PEP 3333).
	source := latin1(r.Header.Get("X-Real-IP"))
	if source == "" {
		source = remoteAddr(r)
	}
	if source == "" {
		source = "-"
	}
	return LogText(source, 64)
}

// latin1 decodes bytes as ISO-8859-1: every byte is one character.
func latin1(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	runes := make([]rune, len(s))
	for i := 0; i < len(s); i++ {
		runes[i] = rune(s[i])
	}
	return string(runes)
}

// remoteAddr is the peer IP without the port, like Flask's request.remote_addr.
func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
