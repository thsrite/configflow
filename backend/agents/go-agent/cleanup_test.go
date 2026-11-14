package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCompactAgentLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	want := bytes.Repeat([]byte("z"), agentLogTail)
	data := append(bytes.Repeat([]byte("a"), agentLogLimit), want...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	before, _ := f.Stat()
	if err := compactAgentLog(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Fatal("changed inode held by service")
	}
	if _, err := f.WriteString("new"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, append(want, []byte("new")...)) {
		t.Fatal("tail or append lost")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := compactAgentLog(link); err == nil {
		t.Fatal("followed symlink")
	}
	if err := compactAgentLog(filepath.Join(dir, "missing")); err != nil {
		t.Fatal(err)
	}
	if err := compactAgentLog(path); err != nil {
		t.Fatal(err)
	}
	gotAgain, _ := os.ReadFile(path)
	if !bytes.Equal(got, gotAgain) {
		t.Fatal("small log changed")
	}
}

func TestCompactLogKeepsRequestedTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	data := append(bytes.Repeat([]byte("a"), 100), []byte("0123456789")...)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := compactLog(path, 200, 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !bytes.Equal(got, data) {
		t.Fatal("log below its limit changed")
	}
	if err := compactLog(path, 50, 10); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "0123456789" {
		t.Fatalf("tail = %q", got)
	}
}
