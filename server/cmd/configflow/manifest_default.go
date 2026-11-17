//go:build !configflow_proddiff

package main

import "configflow/internal/httpx"

func writeTestManifest(*httpx.App) error { return nil }
