// Command fixture creates synthetic fault-harness repositories. It never opens
// unmarked directories, contacts a device or reads a production snapshot.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"configflow/internal/faultprobe"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func main() {
	mode := flag.String("mode", "catalog", "catalog or seed")
	dir := flag.String("dir", "", "harness-owned empty directory")
	source := flag.String("source", "", "loopback source base URL")
	dav := flag.Bool("dav", false, "enable synthetic WebDAV")
	flag.Parse()
	if err := run(*mode, *dir, *source, *dav); err != nil {
		logx.Get("fault-fixture").Error(err.Error())
		os.Exit(1)
	}
}

func run(mode, dir, source string, dav bool) error {
	if mode == "catalog" {
		return json.NewEncoder(os.Stdout).Encode(struct {
			Points map[string]faultprobe.Phase `json:"points"`
			Header string                      `json:"header"`
			Env    string                      `json:"env"`
		}{faultprobe.Catalog(), faultprobe.TokenHeader, faultprobe.ControlEnv})
	}
	if mode != "seed" {
		return errors.New("unknown fixture mode")
	}
	marker, err := os.ReadFile(filepath.Join(dir, ".faults-owned"))
	if err != nil || string(marker) != "configflow-faults-v1\n" {
		return errors.New("refusing unmarked data directory")
	}
	if _, err := os.Stat(filepath.Join(dir, repo.DatabaseFilename)); !os.IsNotExist(err) {
		return errors.New("refusing existing database")
	}
	u, err := url.Parse(source)
	if err != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() {
		return errors.New("fixture source must be HTTP loopback")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return errors.New("fixture requires an explicit loopback port")
	}
	raw, err := json.Marshal(map[string]any{
		"system_config": map[string]any{
			"config_token":      "fault-config-token",
			"rule_proxy_token":  "fault-rule-token",
			"sub_store_enabled": false,
		},
		"backup": map[string]any{
			"auto_backup":     dav,
			"webdav_url":      source + "/dav",
			"webdav_path":     "/backup/",
			"webdav_username": "synthetic",
			"webdav_password": "synthetic",
		},
		"subscriptions": []any{map[string]any{
			"id":              "feed",
			"name":            "Synthetic",
			"enabled":         true,
			"url":             source + "/subscription",
			"user_agent":      "fault-harness",
			"update_interval": 86400,
		}},
		"rule_library": []any{map[string]any{
			"id":          "rulesource",
			"name":        "Synthetic",
			"behavior":    "domain",
			"format":      "text",
			"source_type": "url",
			"url":         source + "/rules",
			"enabled":     true,
		}},
		"proxy_groups": []any{map[string]any{
			"id":            "proxy",
			"name":          "PROXY",
			"type":          "select",
			"subscriptions": []any{"feed"},
			"enabled":       true,
		}},
		"rule_configs": []any{
			map[string]any{"id": "set", "itemType": "ruleset", "library_rule_id": "rulesource", "policy": "PROXY", "enabled": true},
			map[string]any{"id": "last", "itemType": "rule", "rule_type": "MATCH", "value": "", "policy": "DIRECT", "enabled": true},
		},
		"domain_discovery": map[string]any{"proxy_ruleset": "rulesource", "auto_probe": false, "auto_apply": false},
		"mihomo":           map[string]any{"custom_config": "mixed-port: 7890\nmode: rule\nproxies: []\nproxy-groups: []\nrules:\n  - MATCH,DIRECT\n"},
		"agents": []any{map[string]any{
			"id":                       "edge",
			"name":                     "Synthetic",
			"service_type":             "mihomo",
			"profile_id":               "default",
			"host":                     "127.0.0.1",
			"port":                     port,
			"token":                    "fault-synthetic-agent-token",
			"domain_discovery_enabled": true,
			"version":                  "1.8.99",
			"traffic_history_enabled":  false,
		}},
	})
	if err != nil {
		return err
	}
	cfg, err := model.DecodeConfig(raw)
	if err != nil {
		return err
	}
	_, err = repo.Open(dir, repo.Options{InitialSettings: func() *model.Config { return cfg }})
	return err
}
