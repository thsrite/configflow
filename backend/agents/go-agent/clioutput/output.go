// Package clioutput provides the human-facing output and private diagnostic log
// for one-shot Agent commands. Long-running service logging is not changed.
package clioutput

import (
	"agent/internal/logx"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Options controls terminal presentation independently of full diagnostic data.
type Options struct {
	Verbose bool
	Nested  bool
	Color   bool
	Width   int
	Details io.Writer
}

type Session struct {
	Out     io.Writer
	Options Options
	Path    string
	file    *os.File
	mu      sync.Mutex
	failed  bool
	restore func()
}

// New honors the installer's shared append-only log and dedicated report fd.
// If log creation fails, report the failure and fall back to a private temp log.
func New(operation string, verbose bool) *Session {
	out := os.Stdout
	if fd, err := strconv.Atoi(os.Getenv("CONFIGFLOW_REPORT_FD")); err == nil && fd >= 3 {
		candidate := os.NewFile(uintptr(fd), "configflow-report")
		if candidate != nil {
			if _, err := candidate.Stat(); err == nil {
				if _, err := candidate.Write(nil); err == nil {
					out = candidate
				}
			}
		}
	}
	width := 78
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n >= 40 && n <= 120 {
		width = n
	}
	s := &Session{Out: out, Options: Options{Verbose: verbose || os.Getenv("CONFIGFLOW_VERBOSE") == "1", Nested: os.Getenv("CONFIGFLOW_REPORT_NESTED") == "1", Width: width}}
	// The shell flushes the shared diagnostic log once per stage in verbose
	// mode. It owns expansion while this child keeps fd3 concise.
	if os.Getenv("CONFIGFLOW_CLI_ACTIVE") == "1" {
		s.Options.Verbose = false
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	s.Options.Color = !noColor && os.Getenv("TERM") != "dumb" && terminal(out)
	requested := os.Getenv("CONFIGFLOW_LOG_FILE")
	var err error
	if requested != "" {
		s.file, err = openLog(requested)
	} else {
		dir := os.Getenv("CONFIGFLOW_LOG_DIR")
		if dir == "" {
			dir = "/var/log/configflow"
		}
		if !filepath.IsAbs(dir) {
			err = fmt.Errorf("日志目录必须使用绝对路径：%s", Printable(dir))
		} else if err = os.MkdirAll(dir, 0700); err == nil {
			s.file, err = os.CreateTemp(dir, operation+"-"+time.Now().Format("20060102-150405")+"-*.log")
		}
	}
	if err != nil {
		s.notice("完整日志位置不可用：" + err.Error())
		dir, fallbackErr := os.MkdirTemp("", "configflow-"+operation+"-")
		if fallbackErr == nil {
			s.file, fallbackErr = openLog(filepath.Join(dir, "details.log"))
		}
		if fallbackErr != nil {
			s.notice("无法创建备用完整日志：" + fallbackErr.Error() + "；诊断将显示在终端。")
		} else {
			s.notice("完整日志改存于 " + s.file.Name())
		}
	}
	if s.file != nil {
		s.Path = s.file.Name()
	}
	s.Options.Details = s
	previous := log.Writer()
	var diagnostic io.Writer = s
	// The parent may already redirect stderr into this same log. Keep that
	// writer, avoiding two writes of each startup diagnostic to the same file.
	if sameFile(s.file, os.Stderr) {
		diagnostic = checkedDiagnostic{session: s, writer: os.Stderr}
	} else if s.Options.Verbose && s.file != nil {
		diagnostic = io.MultiWriter(s, s.Out)
	}
	log.SetOutput(diagnostic)
	previousLogx := logx.SetOutput(diagnostic)
	s.restore = func() {
		log.SetOutput(previous)
		logx.SetOutput(previousLogx)
	}
	fmt.Fprintf(s, "\n=== %s | %s ===\n", Printable(operation), time.Now().Format(time.RFC3339))
	return s
}

type checkedDiagnostic struct {
	session *Session
	writer  io.Writer
}

func (w checkedDiagnostic) Write(data []byte) (int, error) {
	n, err := w.writer.Write(data)
	if err != nil {
		w.session.mu.Lock()
		if !w.session.failed {
			w.session.notice("完整日志写入失败：" + err.Error() + "；诊断显示在终端。")
			w.session.failed = true
		}
		w.session.mu.Unlock()
		fmt.Fprintln(w.session.Out, Printable(string(data)))
	}
	return n, err
}

func openLog(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("日志必须使用绝对路径：%s", Printable(path))
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("日志路径不是普通文件：%s", Printable(path))
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	opened, statErr := file.Stat()
	current, lstatErr := os.Lstat(path)
	if statErr != nil || lstatErr != nil || !current.Mode().IsRegular() || !os.SameFile(opened, current) {
		file.Close()
		return nil, fmt.Errorf("日志路径在打开时发生变化：%s", Printable(path))
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func sameFile(a, b *os.File) bool {
	if a == nil || b == nil {
		return false
	}
	ai, ae := a.Stat()
	bi, be := b.Stat()
	return ae == nil && be == nil && os.SameFile(ai, bi)
}

// Write retains the entire diagnostic, without the terminal's shortened pass
// messages. A failed log is announced and subsequent details remain visible.
func (s *Session) Write(data []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file != nil && !s.failed {
		n, err := s.file.Write(data)
		if err == nil {
			return n, nil
		}
		s.failed = true
		s.notice("完整日志写入失败：" + err.Error() + "；后续诊断显示在终端。")
	}
	return fmt.Fprint(s.Out, Printable(string(data))+"\n")
}

func (s *Session) notice(message string) {
	fmt.Fprintln(s.Out)
	Wrap(s.Out, "  ! ", message, s.Options.Width)
}

func (s *Session) Close() {
	if s.restore != nil {
		s.restore()
	}
	if s.file != nil {
		if err := s.file.Close(); err != nil {
			s.notice("关闭完整日志失败：" + err.Error())
		}
	}
	if !s.Options.Nested && s.Path != "" {
		fmt.Fprintln(s.Out)
		// Keep paths intact for copying into cat/tail even in a narrow terminal.
		fmt.Fprintln(s.Out, "  完整日志  "+Printable(s.Path))
		if !s.Options.Verbose {
			fmt.Fprintln(s.Out, "  加 --verbose 可展开检查依据。")
		}
	}
}

func (s *Session) Title(title string) {
	if !s.Options.Nested {
		fmt.Fprintf(s.Out, "\nConfigFlow · %s\n\n", Printable(title))
	}
}

// Result is for a one-shot operation without a multi-check report.
func (s *Session) Result(level, title, message, fix string) {
	label := "通过"
	if level == "warn" {
		label = "提醒"
	} else if level == "error" {
		label = "失败"
	}
	fmt.Fprintf(s, "[%s] %s：%s\n", label, Printable(title), Printable(message))
	if fix != "" {
		fmt.Fprintf(s, "  处理方法：%s\n", Printable(fix))
	}
	Row(s.Out, s.Options, level, title)
	if message != "" && (level != "pass" || s.Options.Verbose) {
		Wrap(s.Out, "    原因：", message, s.Options.Width)
	}
	if fix != "" && (level != "pass" || s.Options.Verbose) {
		Wrap(s.Out, "    处理方法：", fix, s.Options.Width)
	}
}

func Row(out io.Writer, options Options, level, text string) {
	symbol, color := "✓", "32"
	if level == "warn" {
		symbol, color = "!", "33"
	} else if level == "error" {
		symbol, color = "✗", "31"
	}
	if options.Color {
		var row strings.Builder
		Wrap(&row, "  "+symbol+" ", text, options.Width)
		fmt.Fprint(out, strings.Replace(row.String(), symbol, "\x1b["+color+"m"+symbol+"\x1b[0m", 1))
	} else {
		Wrap(out, "  "+symbol+" ", text, options.Width)
	}
}

// Printable prevents filenames and diagnostic strings forging terminal output.
func Printable(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, value)
}

// Wrap uses display cells rather than byte length, so Chinese and ASCII reports
// fit the same terminal. Continuation lines retain the original indentation.
// Lines break between Chinese characters or at spaces, never inside an ASCII
// word or path, and never put closing punctuation at the start of a line; a
// word wider than the whole line is still split.
func Wrap(out io.Writer, prefix, text string, width int) {
	if width < 20 {
		width = 78
	}
	prefixWidth := cells(prefix)
	continuation := strings.Repeat(" ", prefixWidth)
	line, used := prefix, prefixWidth
	flush := func() {
		fmt.Fprintln(out, strings.TrimRight(line, " "))
		line, used = continuation, prefixWidth
	}
	for _, unit := range wrapUnits(Printable(text)) {
		n := cells(unit)
		if unit == " " {
			if used+n > width {
				flush()
			} else if used > prefixWidth {
				line += unit
				used += n
			}
			continue
		}
		if n <= width-prefixWidth {
			if used+n > width && used > prefixWidth {
				flush()
			}
			line += unit
			used += n
			continue
		}
		// Wider than any line: split it, starting on the current line.
		for _, r := range unit {
			rn := runeCells(r)
			if used+rn > width && used > prefixWidth {
				flush()
			}
			line += string(r)
			used += rn
		}
	}
	fmt.Fprintln(out, line)
}

const (
	closingPunctuation = "，。、；：！？）」』】》〉”’…,.;:!?)]}%"
	openingPunctuation = "（「『【《〈“‘([{"
)

// wrapUnits splits text into the pieces a line may not break inside: runs of
// ASCII non-space characters, single wide characters and single spaces, with
// closing punctuation kept on the piece before it and opening punctuation on
// the piece after it.
func wrapUnits(text string) []string {
	var units []string
	var word strings.Builder
	opening := ""
	push := func(unit string) {
		units = append(units, opening+unit)
		opening = ""
	}
	endWord := func() {
		if word.Len() > 0 {
			push(word.String())
			word.Reset()
		}
	}
	for _, r := range text {
		switch {
		case r == ' ':
			endWord()
			if opening != "" {
				push("")
			}
			units = append(units, " ")
		case strings.ContainsRune(closingPunctuation, r) && (word.Len() > 0 || len(units) > 0):
			if word.Len() > 0 && r < 0x80 {
				word.WriteRune(r)
				continue
			}
			endWord()
			if last := len(units) - 1; last >= 0 && units[last] != " " {
				units[last] += string(r)
			} else {
				push(string(r))
			}
		case strings.ContainsRune(openingPunctuation, r) && r >= 0x80:
			endWord()
			opening += string(r)
		case r < 0x80:
			word.WriteRune(r)
		default:
			endWord()
			push(string(r))
		}
	}
	endWord()
	if opening != "" {
		push("")
	}
	return units
}

func cells(value string) int {
	result := 0
	for _, r := range value {
		result += runeCells(r)
	}
	return result
}

func runeCells(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115f || r >= 0x2e80 && r <= 0xa4cf || r >= 0xac00 && r <= 0xd7a3 || r >= 0xf900 && r <= 0xfaff || r >= 0xfe10 && r <= 0xfe6f || r >= 0xff00 && r <= 0xff60 || r >= 0xffe0 && r <= 0xffe6 || r >= 0x1f300) {
		return 2
	}
	return 1
}
