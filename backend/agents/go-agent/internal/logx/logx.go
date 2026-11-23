// Package logx provides leveled, redacted logging for the standalone go-agent
// module without depending on server/internal. Line formatting matches the
// panel's logx format so the log viewer highlights levels consistently:
//
//	2026-11-23 12:34:56,789 - agent/routes - INFO - message
//
// Logger names follow Go package paths (for example "agent", "agent/routes",
// "agent/svc", "agent/upgrade"). Every emitted message and structured attribute
// value is redacted so tokens, passwords, Authorization headers, credential-bearing
// URLs, and private keys cannot enter logs.
package logx

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Level is the severity threshold for Agent logs.
type Level int

const (
	Debug Level = 10
	Info  Level = 20
	Warn  Level = 30
	Error Level = 40
)

// String returns the canonical level label used in formatted log lines.
func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warn:
		return "WARNING"
	case Error:
		return "ERROR"
	default:
		return fmt.Sprintf("LEVEL(%d)", int(l))
	}
}

// ParseLevel maps a case-insensitive level name to a Level; unknown names fall
// back to Info.
func ParseLevel(name string) Level {
	switch strings.ToUpper(strings.TrimSpace(name)) {
	case "DEBUG":
		return Debug
	case "WARN", "WARNING":
		return Warn
	case "ERROR":
		return Error
	default:
		return Info
	}
}

// LevelFromEnv reads the initial log level from CONFIGFLOW_AGENT_DEBUG (truthy
// enables Debug) and LOG_LEVEL (defaults to Info).
func LevelFromEnv() Level {
	if raw := strings.TrimSpace(os.Getenv("CONFIGFLOW_AGENT_DEBUG")); raw != "" {
		if enabled, err := strconv.ParseBool(raw); err == nil && enabled {
			return Debug
		}
	}
	if raw := strings.TrimSpace(os.Getenv("LOG_LEVEL")); raw != "" {
		return ParseLevel(raw)
	}
	return Info
}

var (
	stateMu sync.Mutex
	out     io.Writer        = os.Stderr
	minLvl  Level            = LevelFromEnv()
	clockFn func() time.Time = time.Now
)

// SetOutput swaps the process-wide log destination and returns the previous
// writer. Passing nil resets output to os.Stderr.
func SetOutput(w io.Writer) io.Writer {
	stateMu.Lock()
	defer stateMu.Unlock()
	prev := out
	if w == nil {
		out = os.Stderr
	} else {
		out = w
	}
	return prev
}

// Writer returns the current process-wide log destination.
func Writer() io.Writer {
	stateMu.Lock()
	defer stateMu.Unlock()
	return out
}

// SetLevel swaps the process-wide minimum log level and returns the previous
// level.
func SetLevel(l Level) Level {
	stateMu.Lock()
	defer stateMu.Unlock()
	prev := minLvl
	minLvl = l
	return prev
}

// CurrentLevel returns the current process-wide minimum log level.
func CurrentLevel() Level {
	stateMu.Lock()
	defer stateMu.Unlock()
	return minLvl
}

// SetClock replaces the timestamp source (primarily for tests) and returns the
// previous clock. Passing nil restores time.Now.
func SetClock(fn func() time.Time) func() time.Time {
	stateMu.Lock()
	defer stateMu.Unlock()
	prev := clockFn
	if fn == nil {
		clockFn = time.Now
	} else {
		clockFn = fn
	}
	return prev
}

// Logger emits leveled, redacted log lines under a fixed package-path name.
type Logger struct {
	name string
}

// Get returns a named Logger (use Go package paths such as "agent",
// "agent/routes", "agent/svc", "agent/upgrade").
func Get(name string) *Logger {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "agent"
	}
	return &Logger{name: name}
}

// Name returns the logger's package-path name.
func (l *Logger) Name() string {
	return l.name
}

// Enabled reports whether records at level will be emitted.
func (l *Logger) Enabled(level Level) bool {
	stateMu.Lock()
	defer stateMu.Unlock()
	return level >= minLvl
}

// Log writes a structured message with optional alternating key/value pairs.
func (l *Logger) Log(level Level, msg string, kv ...any) {
	if !l.Enabled(level) {
		return
	}
	var b strings.Builder
	b.WriteString(Redact(msg))
	for i := 0; i < len(kv); i += 2 {
		b.WriteByte(' ')
		key := fmt.Sprint(kv[i])
		if i+1 >= len(kv) {
			b.WriteString(Redact(key))
			b.WriteString("=<missing>")
			break
		}
		b.WriteString(key)
		b.WriteByte('=')
		if isSensitiveKey(key) {
			b.WriteString("[REDACTED]")
		} else {
			b.WriteString(Redact(fmt.Sprint(kv[i+1])))
		}
	}
	l.emit(level, b.String())
}

// Logf formats a message with fmt.Sprintf when level is enabled, redacts it,
// and writes it.
func (l *Logger) Logf(level Level, format string, args ...any) {
	if !l.Enabled(level) {
		return
	}
	l.emit(level, Redact(fmt.Sprintf(format, args...)))
}

func (l *Logger) emit(level Level, message string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if level < minLvl {
		return
	}
	now := clockFn()
	line := FormatLine(now, l.name, level, message) + "\n"
	_, _ = io.WriteString(out, line)
}

// FormatLine renders a single log line without trailing newline.
func FormatLine(t time.Time, name string, level Level, message string) string {
	return fmt.Sprintf("%s,%03d - %s - %s - %s",
		t.Format("2006-01-02 15:04:05"),
		t.Nanosecond()/1_000_000,
		name,
		level,
		message,
	)
}

func (l *Logger) Debug(msg string, kv ...any) { l.Log(Debug, msg, kv...) }
func (l *Logger) Info(msg string, kv ...any)  { l.Log(Info, msg, kv...) }
func (l *Logger) Warn(msg string, kv ...any)  { l.Log(Warn, msg, kv...) }
func (l *Logger) Error(msg string, kv ...any) { l.Log(Error, msg, kv...) }

func (l *Logger) Debugf(format string, args ...any) { l.Logf(Debug, format, args...) }
func (l *Logger) Infof(format string, args ...any)  { l.Logf(Info, format, args...) }
func (l *Logger) Warnf(format string, args ...any)  { l.Logf(Warn, format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.Logf(Error, format, args...) }

// Fatalf logs at Error level and terminates the process with exit code 1.
// Only use during startup failures.
func (l *Logger) Fatalf(format string, args ...any) {
	l.Logf(Error, format, args...)
	os.Exit(1)
}
