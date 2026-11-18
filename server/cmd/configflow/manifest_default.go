//go:build !configflow_proddiff && !configflow_difftest

package main

import "configflow/internal/httpx"

func writeTestManifest(*httpx.App) error { return nil }
