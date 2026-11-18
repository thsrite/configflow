package lockx

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestSetupSessionSuborder(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LOCKX_ORDER_HELPER") != "1" {
		cmd := exec.Command(os.Args[0], "-test.run=^TestSetupSessionSuborder$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LOCKX_ORDER_HELPER=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("order child: %v\n%s", err, output)
		}
		return
	}
	tests := []struct {
		name         string
		outer, inner Level
		try          bool
		want         int64
	}{
		{"session-to-profile", LevelSetupSession, LevelDevice, false, 0},
		{"session-to-repo", LevelSetupSession, LevelRepository, false, 0},
		{"profile-to-session", LevelDevice, LevelSetupSession, false, 1},
		{"reverse-try-only", LevelDevice, LevelSetupSession, true, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outer := NewMutex(test.outer, "outer-"+test.name)
			held, release := lockMutex(t, outer, context.Background())
			defer release()
			inner := NewMutex(test.inner, "inner-"+test.name)
			before := Violations()
			if test.try {
				_, unlock, ok := inner.TryLock(held)
				if !ok {
					t.Fatal("free try lock refused")
				}
				unlock()
			} else {
				_, unlock, err := inner.Lock(held, time.Second)
				if err != nil {
					t.Fatal(err)
				}
				unlock()
			}
			if delta := Violations() - before; delta != test.want {
				t.Fatalf("violations=%d want=%d", delta, test.want)
			}
		})
	}
}
