package installer

// Store cases of backend/test_agent_install_commands.py that monkeypatch
// time.time or call the store directly (module A10): the clock is Store.Now.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopedLinksCanExpireEarlier(t *testing.T) {
	ctx := context.Background()
	now := int64(1_800_000_000)
	s := &Store{DataDir: t.TempDir(), Now: func() int64 { return now }}
	deadline := now + 60
	token, expires, err := s.SaveInstallScript(ctx, "#!/bin/sh\nexit 0\n", &deadline)
	if err != nil || expires != now+60 {
		t.Fatalf("%v %d", err, expires)
	}
	s.Now = func() int64 { return now + 59 }
	if _, ok, err := s.LoadInstallScript(ctx, token); !ok || err != nil {
		t.Fatalf("before deadline: %v %v", ok, err)
	}
	s.Now = func() int64 { return now + 60 }
	if _, ok, _ := s.LoadInstallScript(ctx, token); ok {
		t.Fatal("served at the deadline")
	}
}

// test_invalid_scoped_link_lifetimes_do_not_write_snapshots: True and
// 'later' are ruled out by the int64 parameter.
func TestInvalidScopedLinkLifetimesDoNotWriteSnapshots(t *testing.T) {
	for _, delta := range []int64{0, -1, 86401} {
		dir := t.TempDir()
		s := &Store{DataDir: dir, Now: func() int64 { return 100 }}
		deadline := 100 + delta
		if _, _, err := s.SaveInstallScript(context.Background(), "#!/bin/sh\nexit 0\n", &deadline); err == nil {
			t.Errorf("delta %d accepted", delta)
		} else if _, isValue := err.(*ValidationError); !isValue {
			t.Errorf("delta %d: %T %v", delta, err, err)
		}
		if count := snapshotCount(t, s); count != 0 {
			t.Errorf("delta %d: %d snapshots", delta, count)
		}
	}
}

func snapshotCount(t *testing.T, s *Store) int {
	t.Helper()
	if _, err := os.Stat(s.DatabasePath()); err != nil {
		return 0
	}
	c, err := s.open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var n int
	if err := c.conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM snapshots").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLinkExpiresAt24HoursAndIsPruned(t *testing.T) {
	ctx := context.Background()
	now := int64(1_800_000_123)
	s := &Store{DataDir: t.TempDir(), Now: func() int64 { return now }}
	token, expires, err := s.SaveInstallScript(ctx, "#!/bin/sh\necho hi\n", nil)
	if err != nil || expires != now+86400 {
		t.Fatalf("%v %d", err, expires)
	}
	s.Now = func() int64 { return expires - 1 }
	if _, ok, _ := s.LoadInstallScript(ctx, token); !ok {
		t.Fatal("not served before expiry")
	}
	s.Now = func() int64 { return expires }
	if _, ok, _ := s.LoadInstallScript(ctx, token); ok {
		t.Fatal("served at expiry")
	}
	if n := snapshotCount(t, s); n != 0 {
		t.Fatalf("%d snapshots left", n)
	}
}

func TestSnapshotsSurviveANewStoreAndUnsafeTokensAreUnknown(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	token, _, err := (&Store{DataDir: dir}).SaveInstallScript(ctx, "#!/bin/sh\necho persisted\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	other := &Store{DataDir: dir}
	if script, ok, err := other.LoadInstallScript(ctx, token); !ok || err != nil || script != "#!/bin/sh\necho persisted\n" {
		t.Fatalf("%q %v %v", script, ok, err)
	}
	if info, _ := os.Stat(other.DatabasePath()); info.Mode().Perm() != 0o600 {
		t.Errorf("database mode %v", info.Mode())
	}
	if info, _ := os.Stat(filepath.Dir(other.DatabasePath())); info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode %v", info.Mode())
	}
	for _, bad := range []string{"../../config.json", strings.Repeat("A", 10000)} {
		if _, ok, err := other.LoadInstallScript(ctx, bad); ok || err != nil {
			t.Errorf("%.20q: %v %v", bad, ok, err)
		}
	}
}
