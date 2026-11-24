package logx

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoggerFormattingAndLevels(t *testing.T) {
	var buf bytes.Buffer
	prevOut := SetOutput(&buf)
	prevLvl := SetLevel(Info)
	prevClock := SetClock(func() time.Time {
		return time.Date(2026, 11, 23, 12, 34, 56, 789_000_000, time.UTC)
	})
	t.Cleanup(func() {
		SetOutput(prevOut)
		SetLevel(prevLvl)
		SetClock(prevClock)
	})

	logger := Get("agent/routes")
	logger.Log(Debug, "hidden debug")
	logger.Infof("request handled %d", 200)
	logger.Warn("slow upstream", "host", "example.test")
	logger.Error("failed", "token", "top-secret-value")

	got := buf.String()
	if strings.Contains(got, "hidden debug") {
		t.Fatalf("debug message emitted at Info level: %q", got)
	}
	wantLines := []string{
		"2026-11-23 12:34:56,789 - agent/routes - INFO - request handled 200",
		"2026-11-23 12:34:56,789 - agent/routes - WARNING - slow upstream host=example.test",
		"2026-11-23 12:34:56,789 - agent/routes - ERROR - failed token=[REDACTED]",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("missing expected line %q in %q", want, got)
		}
	}
	if strings.Contains(got, "top-secret-value") {
		t.Fatalf("sensitive attribute leaked: %q", got)
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		secrets []string
		wantSub string
	}{
		{
			name:    "token query in subscription url",
			input:   "fetching https://sub.example.test/api/v1?token=sub-secret-99&flag=clash",
			secrets: []string{"sub-secret-99"},
			wantSub: "token=[REDACTED]",
		},
		{
			name:    "userinfo password in url",
			input:   "download https://alice:hunter2@sub.example.test/file.yaml?config_token=cfg-tok-42",
			secrets: []string{"alice", "hunter2", "cfg-tok-42"},
			wantSub: "config_token=[REDACTED]",
		},
		{
			name:    "double encoded nested token in url",
			input:   "url https://example.test/p?next=https%253A%252F%252Fnested.test%252Fp%253Ftoken%253Dnested-secret",
			secrets: []string{"nested-secret"},
			wantSub: "next=[REDACTED]",
		},
		{
			name:    "authorization bearer header",
			input:   "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.payload.sig",
			secrets: []string{"eyJhbGciOiJIUzI1NiJ9", "payload.sig"},
			wantSub: "[REDACTED]",
		},
		{
			name:    "authorization basic header",
			input:   "Authorization=Basic dXNlcjpwYXNz",
			secrets: []string{"dXNlcjpwYXNz"},
			wantSub: "[REDACTED]",
		},
		{
			name:    "bare bearer token",
			input:   "rejected Bearer bare-token-abcdef1234",
			secrets: []string{"bare-token-abcdef1234"},
			wantSub: "Bearer [REDACTED]",
		},
		{
			name:    "password key value",
			input:   "login failed password=my-secret-pass user=admin",
			secrets: []string{"my-secret-pass"},
			wantSub: "password=[REDACTED]",
		},
		{
			name:    "json token and password",
			input:   `payload {"token": "tok-json-1", "password": "pw-json-2", "name": "ok"}`,
			secrets: []string{"tok-json-1", "pw-json-2"},
			wantSub: `"token": "[REDACTED]"`,
		},
		{
			name: "multiline pem private key",
			input: "loaded key:\n-----BEGIN OPENSSH PRIVATE KEY-----\n" +
				"b3BlbnNzaC1rZXktdjEAAAAABG5vbmU=\n" +
				"-----END OPENSSH PRIVATE KEY-----\ndone",
			secrets: []string{"b3BlbnNzaC1rZXktdjEAAAAABG5vbmU="},
			wantSub: "[PRIVATE KEY REDACTED]",
		},
		{
			name:    "inline private_key kv",
			input:   "wg config private_key=abc123privkeyxyz",
			secrets: []string{"abc123privkeyxyz"},
			wantSub: "private_key=[REDACTED]",
		},
		{
			name:    "registration key kv json and env",
			input:   `registration_key=rk-secret-1 {"registration_key":"rk-secret-2"} AGENT_REGISTRATION_KEY=rk-secret-3`,
			secrets: []string{"rk-secret-1", "rk-secret-2", "rk-secret-3"},
			wantSub: "registration_key=[REDACTED]",
		},
		{
			name:    "hyphenated private-key and api-key",
			input:   "wireguard private-key = WGpriv11= api-key: ak-secret-12",
			secrets: []string{"WGpriv11", "ak-secret-12"},
			wantSub: "private-key = [REDACTED]",
		},
		{
			name:    "markers keep a single closing bracket",
			input:   "Authorization: Bearer abcdefghij8 then https://sub.example.test/s?token=sub-tok-5",
			secrets: []string{"abcdefghij8", "sub-tok-5", "]]"},
			wantSub: "Authorization: [REDACTED] then https://sub.example.test/s?token=[REDACTED]",
		},
		{
			name:    "control characters neutralized",
			input:   "hello\r\n2026-01-01 00:00:00,000 - agent - INFO - forged",
			secrets: []string{"\r", "\n"},
			wantSub: "hello  2026-01-01",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Redact(tc.input)
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Fatalf("secret %q leaked in %q", secret, got)
				}
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Fatalf("expected %q in %q", tc.wantSub, got)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want Level
	}{
		{"DEBUG", Debug},
		{"debug", Debug},
		{"INFO", Info},
		{"WARN", Warn},
		{"WARNING", Warn},
		{"ERROR", Error},
		{"unknown", Info},
		{"", Info},
	}
	for _, tc := range tests {
		if got := ParseLevel(tc.in); got != tc.want {
			t.Errorf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestConcurrentLoggingAndSetOutput(t *testing.T) {
	prevOut := SetOutput(&bytes.Buffer{})
	prevLvl := SetLevel(Debug)
	t.Cleanup(func() {
		SetOutput(prevOut)
		SetLevel(prevLvl)
	})

	logger := Get("agent/test")
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 25 {
				logger.Infof("worker %d iter %d token=secret-%d-%d", id, j, id, j)
			}
		}(i)
	}
	wg.Wait()
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
