//go:build !configflow_proddiff && !configflow_difftest

package main

import (
	"configflow/internal/httpx"
	"configflow/internal/provider"
)

func writeTestManifest(*httpx.App, *provider.Service) error { return nil }
