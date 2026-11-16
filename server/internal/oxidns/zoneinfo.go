package oxidns

import (
	"bytes"
	"os"
	"strings"
	"sync"
)

// zoneinfo.ZoneInfo(key) as the validators use it: ok=false is
// ZoneInfoNotFoundError or ValueError (both caught by the callers).
//
// CPython 3.11 resolves a key against TZPATH (PYTHONTZPATH, else the build
// default /usr/share/zoneinfo:/usr/lib/zoneinfo:/usr/share/lib/zoneinfo:
// /etc/zoneinfo) after _validate_tzfile_path, opens the first regular file
// and requires the TZif magic; the tzdata package is the fallback, and it is
// not installed in the backend's environment. So validity depends on the
// host's zoneinfo directory exactly as for the Python server (the Go image
// must ship tzdata like the Python image does).
//
// Difference: only the TZif magic is checked; CPython parses the whole file
// (a truncated file under TZPATH would be a ValueError there).

var (
	tzpathOnce sync.Once
	tzpath     []string
)

func zoneInfoPath() []string {
	tzpathOnce.Do(func() {
		raw, set := os.LookupEnv("PYTHONTZPATH")
		if !set {
			raw = "/usr/share/zoneinfo:/usr/lib/zoneinfo:/usr/share/lib/zoneinfo:/etc/zoneinfo"
		}
		for _, entry := range strings.Split(raw, ":") {
			if entry != "" && strings.HasPrefix(entry, "/") {
				tzpath = append(tzpath, entry)
			}
		}
	})
	return tzpath
}

// posixNormpath is posixpath.normpath(p).
func posixNormpath(p string) string {
	if p == "" {
		return "."
	}
	initial := 0
	if strings.HasPrefix(p, "/") {
		initial = 1
		if strings.HasPrefix(p, "//") && !strings.HasPrefix(p, "///") {
			initial = 2
		}
	}
	var parts []string
	for _, comp := range strings.Split(p, "/") {
		if comp == "" || comp == "." {
			continue
		}
		if comp != ".." || (initial == 0 && len(parts) == 0) || (len(parts) > 0 && parts[len(parts)-1] == "..") {
			parts = append(parts, comp)
		} else if len(parts) > 0 {
			parts = parts[:len(parts)-1]
		}
	}
	result := strings.Repeat("/", initial) + strings.Join(parts, "/")
	if result == "" {
		return "."
	}
	return result
}

// zoneInfoValid is "ZoneInfo(key) does not raise ZoneInfoNotFoundError or
// ValueError".
func zoneInfoValid(key string) bool {
	if strings.HasPrefix(key, "/") {
		return false
	}
	normalized := posixNormpath(key)
	if runeLen(normalized) != runeLen(key) {
		return false
	}
	if !strings.HasPrefix(posixNormpath("_/"+normalized), "_/") {
		return false
	}
	if strings.ContainsRune(key, 0) {
		return false
	}
	for _, dir := range zoneInfoPath() {
		file := dir + "/" + key
		if strings.HasSuffix(dir, "/") {
			file = dir + key
		}
		info, err := os.Stat(file)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		handle, err := os.Open(file)
		if err != nil {
			return false
		}
		magic := make([]byte, 4)
		n, _ := handle.Read(magic)
		handle.Close()
		return n == 4 && bytes.Equal(magic, []byte("TZif"))
	}
	return false
}
