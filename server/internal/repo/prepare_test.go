package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareConfigDefersAgentInitialization(t *testing.T) {
	r := newRepo(t)
	ctx := WithScope(WithRepository(context.Background(), r))
	defer ResetConfigContext(ctx)
	initializations := 0
	previous := SetHooks(Hooks{InitAgentManager: func() { initializations++ }})
	defer SetHooks(previous)
	prepared, err := PrepareConfig(ctx)
	if err != nil || prepared == nil || initializations != 0 {
		t.Fatalf("prepare: document=%v, init=%d, err=%v", prepared != nil, initializations, err)
	}
	loaded, err := LoadConfig(ctx)
	if err != nil || loaded == nil || initializations != 1 {
		t.Fatalf("legacy load: document=%v, init=%d, err=%v", loaded != nil, initializations, err)
	}
}

func TestResolveDataDirMatchesOpenWithoutOpening(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	actual := filepath.Join(home, "data")
	link := filepath.Join(home, "alias")
	if err := os.Symlink(actual, link); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{actual, "~/data", link} {
		resolved, err := ResolveDataDir(path)
		if err != nil || resolved != actual {
			t.Fatalf("resolve %q: %q %v", path, resolved, err)
		}
		if _, err := os.Stat(actual); !os.IsNotExist(err) {
			t.Fatalf("resolution created storage: %v", err)
		}
	}
	r, err := OpenDefault("~/data")
	if err != nil || r.DataDir() != actual {
		t.Fatalf("Open resolved differently: %v", err)
	}
}
