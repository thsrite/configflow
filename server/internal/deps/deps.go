// Package deps pins the third-party modules the port depends on.
//
// Work packages are written in parallel; if each of them added its own
// requirement, go.mod and go.sum would conflict on every merge. Everything
// the port is expected to need is therefore required here once, through
// blank imports, so `go mod tidy` keeps the entries even while no other
// package imports them yet. Do not import this package.
package deps

import (
	_ "github.com/dlclark/regexp2"
	_ "github.com/klauspost/compress/zstd"
	_ "golang.org/x/net/publicsuffix"
	_ "golang.org/x/sys/unix"
	_ "gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)
