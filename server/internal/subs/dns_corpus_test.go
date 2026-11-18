package subs

import (
	"strconv"
	"testing"

	"configflow/internal/jsonx"
)

func TestProxyServerDNSCorpus(t *testing.T) {
	rows := loadCorpus(t, "dns.jsonl.gz")
	native := nativeDNSExpectations(t)
	kinds := map[string]int{}
	for i, row := range rows {
		kinds[row.Fn]++
		if entry := native.GetObject(strconv.Itoa(i)); entry != nil {
			row.Out = entry.Value("out")
		}
		switch row.Fn {
		case "compress":
			got := jsonx.ListOf(Compress(strList(row.In)))
			if g, w := compact(t, got), compact(t, row.Out); g != w {
				t.Fatalf("row %d compress(%s)\n got  %s\n want %s", i, compact(t, row.In), g, w)
			}
		case "listener_address":
			addr, reason := ListenerAddress(row.In)
			var a, r any
			if addr != "" {
				a = addr
			}
			if reason != "" {
				r = reason
			}
			if g, w := compact(t, []any{a, r}), compact(t, row.Out); g != w {
				t.Fatalf("row %d listener_address(%s): got %s want %s", i, compact(t, row.In), g, w)
			}
		case "parse_core_version":
			v, ok := ParseCoreVersion(row.In)
			var got any
			if ok {
				got = []any{jsonx.BigInt(v[0]), jsonx.BigInt(v[1]), jsonx.BigInt(v[2])}
			}
			if g, w := compact(t, got), compact(t, row.Out); g != w {
				t.Fatalf("row %d parse_core_version(%s): got %s want %s", i, compact(t, row.In), g, w)
			}
		case "core_too_old":
			if got := CoreTooOld(row.In); got != row.Out.(bool) {
				t.Fatalf("row %d core_too_old(%s): got %v", i, compact(t, row.In), got)
			}
		case "final_target":
			in := asObj(row.In)
			got := FinalTarget(in.GetObject("hosts"), in.GetString("domain"))
			var g any
			if got != "" {
				g = got
			}
			if gs, ws := compact(t, g), compact(t, row.Out); gs != ws {
				t.Fatalf("row %d final_target(%s): got %s want %s", i, compact(t, row.In), gs, ws)
			}
		case "apply_node_dns":
			in := asObj(row.In)
			main := in.GetObject("main")
			core, _ := in.Value("core_version").(string)
			report := ApplyNodeDNS(main, in.GetObject("merged"), core)
			want := asObj(row.Out)
			var r any
			if report != nil {
				r = report
			}
			if g, w := compact(t, jsonx.Obj("report", r, "main", main)), compact(t, want); g != w {
				t.Fatalf("row %d apply_node_dns\n in   %s\n got  %s\n want %s", i, compact(t, row.In), g, w)
			}
		}
	}
	t.Logf("%v", kinds)
}
