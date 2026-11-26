package main

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func row(t *testing.T, coverage *Map, name string) *Map {
	t.Helper()
	v, ok := coverage.Get("GENERATOR " + name)
	if !ok {
		t.Fatalf("no row %s", name)
	}
	return v.(*Map)
}

func TestGeneratorInventoryAndHTTPTakeover(t *testing.T) {
	doc := docFrom(t, `{"system":{"system_config":{"default_profile_id":"one"},"agents":[{"id":"agent-a","service_type":"mihomo"}]},
		"shared":{"subscriptions":[{"id":"sub-a"}]},"profiles":{"one":{"routers":{"items":[]}},"two":{"routers":{"items":[]}}}}`)
	jobs, coverage := inventory(doc, nil)
	if counter(row(t, coverage, "mihomo"), "cases") != 2 || counter(row(t, coverage, "subscription-loon"), "cases") != 2 ||
		counter(row(t, coverage, "agent-mihomo"), "cases") != 1 {
		t.Fatal(dumpJSON(coverage, dumpOptions{indent: -1}))
	}
	for i, job := range jobs {
		if job.index != i {
			t.Fatal("job indexes")
		}
	}
	jobs, coverage = inventory(doc, map[routeKey]bool{{"GET", "/api/config/<profile_id>/mihomo"}: true})
	if !pyEqual(getOr(row(t, coverage, "mihomo"), "mode", nil), "http") {
		t.Fatal("mihomo not taken over")
	}
	for _, job := range jobs {
		if job.kind == "mihomo" {
			t.Fatal("taken-over generator still runs")
		}
	}
}

func TestSuccessfulOutputFloorSurvivesHTTPTakeover(t *testing.T) {
	previous := docFrom(t, `{"GENERATOR mihomo":{"success":2,"mode":"generator"}}`)
	if lost := successfulOutputRegressions(previous, docFrom(t, `{"GENERATOR mihomo":{"success":2,"mode":"http"}}`)); len(lost) != 0 {
		t.Fatal(lost)
	}
	if lost := successfulOutputRegressions(previous, docFrom(t, `{"GENERATOR mihomo":{"success":1}}`)); !reflect.DeepEqual(lost, []string{"GENERATOR mihomo successful-output-count"}) {
		t.Fatal(lost)
	}
}

func TestPreviousPartialCoverageIsPromotedWithoutLosingOutput(t *testing.T) {
	doc := docFrom(t, `{"system":{"agents":[{"id":"agent-a","service_type":"mihomo"}]},"shared":{},"profiles":{"default":{}}}`)
	_, coverage := inventory(doc, nil)
	if counter(row(t, coverage, "agent-deployment-bundle"), "cases") != 1 || !pyEqual(getOr(row(t, coverage, "aggregation-provider"), "mode", nil), "generator") {
		t.Fatal(dumpJSON(coverage, dumpOptions{indent: -1}))
	}
	key := "GENERATOR agent-deployment-bundle"
	previous := docFrom(t, `{"`+key+`":{"success":2,"partial":1}}`)
	if lost := successfulOutputRegressions(previous, docFrom(t, `{"`+key+`":{"success":3,"partial":0}}`)); len(lost) != 0 {
		t.Fatal(lost)
	}
	if lost := successfulOutputRegressions(previous, docFrom(t, `{"`+key+`":{"success":2,"partial":0}}`)); !reflect.DeepEqual(lost, []string{key + " packaging-output-count"}) {
		t.Fatal(lost)
	}
	lost := successfulOutputRegressions(previous, docFrom(t, `{"`+key+`":{"success":1,"partial":2}}`))
	if len(lost) == 0 || lost[0] != key+" successful-output-count" {
		t.Fatal(lost)
	}
}

func TestGeneratorAggregationSelectorUsesSharedKey(t *testing.T) {
	doc := docFrom(t, `{"shared":{"subscription_aggregations":[{"id":"one"},{"id":"two"}]},"profiles":{"default":{},"other":{}},"system":{}}`)
	jobs, coverage := inventory(doc, nil)
	ids := map[string]bool{}
	n := 0
	for _, job := range jobs {
		if job.kind == "aggregation-provider" {
			n++
			ids[job.aggID] = true
		}
	}
	if n != 4 || len(ids) != 2 || counter(row(t, coverage, "aggregation-provider"), "cases") != 4 {
		t.Fatal(n, ids)
	}
	_, coverage = inventory(doc, map[routeKey]bool{{"GET", "/api/profiles/<profile_id>/aggregations/<agg_id>/provider"}: true})
	if !pyEqual(getOr(row(t, coverage, "aggregation-provider"), "mode", nil), "http") {
		t.Fatal("aggregation not taken over")
	}
}

func TestGeneratorWorkersRunTheBaselineGenerator(t *testing.T) {
	r := newRun(repoRoot(t))
	r.cmp = testComparator(t)
	doc := docFrom(t, `{"system":{"agents":[]},"shared":{},"profiles":{"default":{}}}`)
	var commands [][]string
	r.copySnapshot = func(string, string) error { return nil }
	r.runCommand = func(command, env []string, logPath string, timeout time.Duration) error {
		commands = append(commands, command[:len(command)-2])
		manifest, err := os.ReadFile(command[len(command)-2])
		if err != nil {
			return err
		}
		jobs, err := loadJSON(manifest)
		if err != nil {
			return err
		}
		base := command[0] == "/synthetic/base-generator"
		var lines []string
		for _, job := range jobs.([]any) {
			index := pyStr(getOr(job, "index", nil))
			switch getOr(job, "kind", "") {
			case "mihomo":
				port := "2"
				if base {
					port = "1"
				}
				lines = append(lines, `{"index":`+index+`,"status":"ok","content":"port: `+port+`\n"}`)
			case "surge":
				status := "error"
				if base {
					status = "ok"
				}
				lines = append(lines, `{"index":`+index+`,"status":"`+status+`","content":"x\n","error":"boom"}`)
			default:
				lines = append(lines, `{"index":`+index+`,"status":"ok","content":"{}"}`)
			}
		}
		return os.WriteFile(command[len(command)-1], []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	}
	dir := t.TempDir()
	result, err := r.runWorkers(dir, dir, dir, "/synthetic/candidate-generator", []string{"/synthetic/base-generator"}, doc, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(commands, [][]string{{"/synthetic/base-generator"}, {"/synthetic/candidate-generator"}}) {
		t.Fatal(commands)
	}
	if !result.found[[3]string{"GENERATOR mihomo", "body.port", "value"}] || !result.found[[3]string{"GENERATOR surge", "status", "generation-error-go"}] {
		t.Fatal(sortedEntries(result.found))
	}
	surge := row(t, result.coverage, "surge")
	if counter(surge, "base-error") != 0 || counter(surge, "go-error") != 1 || !surge.Has("base-error") {
		t.Fatal(dumpJSON(surge, dumpOptions{indent: -1}))
	}
	var keys []string
	for k := range result.samples {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, []string{"json", "text", "yaml"}) {
		t.Fatal(keys)
	}
}
