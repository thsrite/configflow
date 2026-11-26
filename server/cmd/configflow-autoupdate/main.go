// Command configflow-autoupdate updates the autoupdate image from source on
// container start, and prepares the image at build time.
//
//	configflow-autoupdate                         start-up update (CONFIGFLOW_AUTO_UPDATE=1; otherwise a no-op)
//	configflow-autoupdate --precompress <dir>     write .gz siblings of the frontend assets
//	configflow-autoupdate --record-image <root> [<out>]
//	                                              record the bundled components' source fingerprints
//
// The start-up update always exits 0: a failure keeps the previous build.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"configflow/internal/autoupdate"
	"configflow/internal/logx"
)

func main() {
	root, err := logx.Setup(logx.Config{Level: logx.Info, Console: os.Stdout})
	code := run(os.Args[1:])
	if err == nil {
		_ = root.Close()
	}
	os.Exit(code)
}

func run(args []string) int {
	switch {
	case len(args) == 2 && args[0] == "--precompress":
		if err := autoupdate.PrecompressDir(args[1]); err != nil {
			logx.Get("auto-update").Error(err.Error())
			return 1
		}
		return 0
	case (len(args) == 2 || len(args) == 3) && args[0] == "--record-image":
		out := autoupdate.FromEnv().ImageTrees
		if len(args) == 3 {
			out = args[2]
		}
		trees, err := autoupdate.RecordImage(args[1], out)
		if err != nil {
			logx.Get("auto-update").Error(err.Error())
			return 1
		}
		logx.Get("auto-update").Info(fmt.Sprintf("recorded image source fingerprints: %v", trees))
		return 0
	case len(args) == 0:
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		autoupdate.New(autoupdate.FromEnv()).Run(ctx)
		return 0
	}
	logx.Get("auto-update").Error("usage: configflow-autoupdate [--precompress <dir> | --record-image <root> [<out>]]")
	return 2
}
