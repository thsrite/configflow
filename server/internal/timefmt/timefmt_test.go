package timefmt

import (
	"testing"
	"time"
)

func TestFormats(t *testing.T) {
	east := time.FixedZone("east", 8*3600)
	odd := time.FixedZone("lmt", 5*3600+17*60+36)
	west := time.FixedZone("west", -(3*3600 + 30*60))
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"naive whole second", Naive(time.Date(2026, 1, 2, 3, 4, 5, 0, east)), "2026-01-02T03:04:05"},
		{"naive drops nanoseconds below a microsecond", Naive(time.Date(2026, 1, 2, 3, 4, 5, 999, time.UTC)), "2026-01-02T03:04:05"},
		{"naive micro", Naive(time.Date(2026, 1, 2, 3, 4, 5, 1000, time.UTC)), "2026-01-02T03:04:05.000001"},
		{"naive full fraction", Naive(time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.UTC)), "2026-01-02T03:04:05.123456"},
		{"naive small year", Naive(time.Date(5, 1, 2, 3, 4, 5, 0, time.UTC)), "0005-01-02T03:04:05"},
		{"offset east", Offset(time.Date(2026, 1, 2, 3, 4, 5, 500000000, east)), "2026-01-02T03:04:05.500000+08:00"},
		{"offset west", Offset(time.Date(2026, 1, 2, 3, 4, 5, 0, west)), "2026-01-02T03:04:05-03:30"},
		{"offset seconds", Offset(time.Date(1900, 1, 2, 3, 4, 5, 0, odd)), "1900-01-02T03:04:05+05:17:36"},
		{"offset utc", Offset(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)), "2026-01-02T03:04:05+00:00"},
		{"utc converts", UTC(time.Date(2026, 1, 2, 3, 4, 5, 0, east)), "2026-01-01T19:04:05+00:00"},
		{"utcz converts", UTCZ(time.Date(2026, 1, 2, 3, 4, 5, 123456789, east)), "2026-01-01T19:04:05.123456Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %q, want %q", tt.got, tt.want)
			}
		})
	}
}
