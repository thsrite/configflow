package routes

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Report of side-gateway return routing (connection marks + policy routing that
// force replies back through the upstream router). Writes live in
// return_routing_manage.go.

// runCommand is replaceable in tests.
var runCommand = func(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		// A bare "exit status 1" hides why the host tool refused, e.g. an ip without JSON support.
		if detail := strings.TrimSpace(string(exitErr.Stderr)); detail != "" {
			err = fmt.Errorf("%w: %s", err, detail)
		}
	}
	return string(out), err
}

// listInterfaces is replaceable in tests.
var listInterfaces = func() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(ifaces))
	for _, iface := range ifaces {
		names = append(names, iface.Name)
	}
	return names
}

type returnTable struct {
	Family string   `json:"family"`
	Name   string   `json:"name"`
	Marks  []string `json:"marks"`
}

type fwmarkRule struct {
	Priority string `json:"priority"`
	Mark     string `json:"mark"`
	Table    string `json:"table"`
}

var (
	nftTableLine  = regexp.MustCompile(`^table\s+(\S+)\s+(\S+)`)
	ctMarkSet     = regexp.MustCompile(`ct mark set ct mark \| (0x[0-9a-fA-F]+)`)
	ipRuleFwmark  = regexp.MustCompile(`^(\d+):.*\bfwmark\s+(\S+).*\blookup\s+(\S+)`)
	tunDeviceName = regexp.MustCompile(`^(Meta|utun\d*|tun\d*|mihomo\d*)$`)
)

// parseNftTables returns "family name" pairs from `nft list tables`.
func parseNftTables(output string) [][2]string {
	var tables [][2]string
	for _, line := range strings.Split(output, "\n") {
		if m := nftTableLine.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			tables = append(tables, [2]string{m[1], m[2]})
		}
	}
	return tables
}

// parseCtMarks returns distinct connection marks set by a table (`ct mark set ct mark | 0x...`).
func parseCtMarks(tableDump string) []string {
	seen := map[string]bool{}
	var marks []string
	for _, m := range ctMarkSet.FindAllStringSubmatch(tableDump, -1) {
		mark := strings.ToLower(m[1])
		if !seen[mark] {
			seen[mark] = true
			marks = append(marks, mark)
		}
	}
	sort.Strings(marks)
	return marks
}

// parseFwmarkRules extracts `ip rule` entries that route by firewall mark.
func parseFwmarkRules(output string) []fwmarkRule {
	var rules []fwmarkRule
	for _, line := range strings.Split(output, "\n") {
		if m := ipRuleFwmark.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			rules = append(rules, fwmarkRule{Priority: m[1], Mark: m[2], Table: m[3]})
		}
	}
	return rules
}

func collectReturnRouting(cfg *Config) map[string]any {
	result := map[string]any{"success": true, "managed": managedReturnStatus(cfg)}
	result["safety"] = returnSafetyStatus(cfg)

	tablesOut, nftErr := runCommand("nft", "list", "tables")
	result["nft_available"] = nftErr == nil
	returnTables := []returnTable{}
	allTables := []string{}
	if nftErr == nil {
		for _, table := range parseNftTables(tablesOut) {
			allTables = append(allTables, table[0]+" "+table[1])
			dump, err := runCommand("nft", "list", "table", table[0], table[1])
			if err != nil {
				continue
			}
			if marks := parseCtMarks(dump); len(marks) > 0 {
				returnTables = append(returnTables, returnTable{Family: table[0], Name: table[1], Marks: marks})
			}
		}
	}
	result["nft_tables"] = allTables
	result["return_tables"] = returnTables

	rulesOut, ipErr := runCommand("ip", "-N", "-4", "rule", "show")
	result["iproute_available"] = ipErr == nil
	rules := []fwmarkRule{}
	routes := map[string][]string{}
	if ipErr == nil {
		rules = parseFwmarkRules(rulesOut)
		for _, rule := range rules {
			if _, done := routes[rule.Table]; done {
				continue
			}
			out, err := runCommand("ip", "route", "show", "table", rule.Table)
			if err != nil {
				continue
			}
			lines := []string{}
			for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
				if line = strings.TrimSpace(line); line != "" {
					lines = append(lines, line)
				}
			}
			routes[rule.Table] = lines
		}
	}
	result["fwmark_rules"] = rules
	result["table_routes"] = routes

	tun := []string{}
	for _, name := range listInterfaces() {
		if tunDeviceName.MatchString(name) {
			tun = append(tun, name)
		}
	}
	result["tun_devices"] = tun
	return result
}

// ReturnRoutingHandler serves /api/return-routing: GET reports, PUT applies the
// desired state, DELETE removes only journaled owned resources. Legacy restoration is rejected.
func ReturnRoutingHandler(cfg *Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			returnRoutingMu.Lock()
			defer returnRoutingMu.Unlock()
			JsonResponse(w, http.StatusOK, collectReturnRouting(cfg))
		case http.MethodPut, http.MethodDelete:
			returnRoutingMu.Lock()
			defer returnRoutingMu.Unlock()
			returnRoutingWrite(cfg, w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}
}

var returnRoutingMu sync.Mutex
