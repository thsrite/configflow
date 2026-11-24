package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/spec"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"configflow/internal/routers/routerfake"
)

// r09ReviewRow finds a fake-device row by ID.
func r09ReviewRow(rows []routerfake.Row, id string) routerfake.Row {
	for _, row := range rows {
		if row[".id"] == id {
			return row
		}
	}
	return nil
}

// r09ReviewOwned returns the manifest resource for role, failing when absent.
func r09ReviewOwned(t *testing.T, raw json.RawMessage, role string) dnsguard.Resource {
	t.Helper()
	m, err := dnsguard.ParseManifest(raw)
	if err != nil || m == nil {
		t.Fatalf("manifest unreadable: %v %s", err, raw)
	}
	for _, e := range m.Resources {
		if e.Role == role {
			return e
		}
	}
	t.Fatalf("role %s not owned: %s", role, raw)
	return dnsguard.Resource{}
}

// A rollback that restores some objects and fails on another must keep owning
// the restored ones; otherwise the next preview is blocked by ConfigFlow's own
// objects (reserved list name / hijack comment without proof).
func TestR09ReviewPartialRollbackKeepsRestoredObjectsOwned(t *testing.T) {
	f, _, d, previous := r09Seed(t)
	changed := r09Base("partial").Settings
	changed.Bypass = []string{"192.0.2.60/32"}
	p := r09Plan(t, d, changed, previous)
	op := p.Ops[0].(*dnsguard.Replace)
	deleted := []string{}
	for _, e := range *op.Delete {
		deleted = append(deleted, e.Role)
	}
	if !reflect.DeepEqual(deleted, []string{"bypass:192.0.2.50/32", "netwatch"}) {
		t.Fatalf("unexpected delta %v", deleted)
	}
	old := r09ReviewOwned(t, previous, "bypass:192.0.2.50/32")
	// The new Netwatch is refused, and so is the compensating re-creation of
	// the old one; the old bypass member is restored under a new ID.
	refused := routerfake.Row{"message": "injected refusal"}
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{
		{Method: "PUT", Path: ros.PathNetwatch, Count: 1, Status: 400, Body: refused},
		{Method: "PUT", Path: ros.PathNetwatch, Count: 2, Status: 400, Body: refused},
	}})
	_, err := dnsguard.Apply(context.Background(), d.Client, *op)
	if err == nil || device.RouterErrorKind(err) != device.KindRollback {
		t.Fatalf("incomplete rollback not reported: %v", err)
	}
	restored := r09ReviewOwned(t, ros.GuardRaw(d.DNSGuardManifest()), "bypass:192.0.2.50/32")
	if restored.ID == old.ID {
		t.Fatalf("restored member still recorded under its deleted ID %s", old.ID)
	}
	row := r09ReviewRow(f.Snapshot().Tables[ros.PathAddresses], restored.ID)
	if row == nil || row["address"] != "192.0.2.50" {
		t.Fatalf("manifest does not point at the restored member: %v", row)
	}
	for _, settings := range []spec.DNSGuard{changed, r09Base("partial").Settings} {
		next := r09Plan(t, d, settings, ros.GuardRaw(d.DNSGuardManifest()))
		if len(next.Blockers) != 0 || next.Status != "pending" || len(next.Ops) != 1 {
			t.Fatalf("own restored objects block the next preview: %s", r09Raw(next))
		}
	}
	r09ApplyPlan(t, d, r09Plan(t, d, changed, ros.GuardRaw(d.DNSGuardManifest())))
}

// The publication-time block is also the continuous detector: a copy of the
// hijack made after publishing (WinBox copy keeps the comment) turns a synced
// guard into a blocked one at the next inspection, and nothing is written.
func TestR09ReviewPostPublishCopyIsDetected(t *testing.T) {
	for _, disabled := range []string{"false", "true"} {
		t.Run("disabled="+disabled, func(t *testing.T) {
			f, _, d, previous := r09Seed(t)
			settings := r09Base("copy").Settings
			if p := r09Plan(t, d, settings, previous); p.Status != "synced" {
				t.Fatalf("seed not synced: %s", r09Raw(p))
			}
			snapshot := f.Snapshot()
			hijack := r09ReviewOwned(t, previous, "hijack-udp")
			source := r09ReviewRow(snapshot.Tables[ros.PathNAT], hijack.ID)
			copied := routerfake.Row{}
			for key, value := range source {
				copied[key] = value
			}
			copied[".id"], copied["disabled"], copied["in-interface"] = "*F1", disabled, "vlan20"
			snapshot.Tables[ros.PathNAT] = append(snapshot.Tables[ros.PathNAT], copied)
			f.ReplaceTables(snapshot.Tables)
			count := len(f.Calls())
			p := r09Plan(t, d, settings, previous)
			if p.Status != "unknown" || !reflect.DeepEqual(p.Blockers, []string{dnsguard.MessageHijackCollision}) || len(p.Ops) != 0 {
				t.Fatalf("post-publish copy not detected: %s", r09Raw(p))
			}
			for _, call := range f.Calls()[count:] {
				if call.Method != "GET" {
					t.Fatalf("detection wrote %s %s", call.Method, call.Path)
				}
			}
			// Withdrawal stays possible: it deletes the Netwatch that selects by comment.
			off := settings
			off.Enabled = false
			p = r09Plan(t, d, off, previous)
			if len(p.Blockers) != 0 || len(p.Ops) != 1 {
				t.Fatalf("copy blocked withdrawal: %s", r09Raw(p))
			}
			r09ApplyPlan(t, d, p)
			after := f.Snapshot()
			if len(after.Tables[ros.PathNetwatch]) != 0 || r09ReviewRow(after.Tables[ros.PathNAT], "*F1") == nil {
				t.Fatalf("withdrawal kept the Netwatch or removed the copy: %v", after.Tables)
			}
		})
	}
}

// Hijacks deleted and restored by a successful rollback carry the comment the
// Netwatch selects; the remapped manifest must own them, so no collision.
func TestR09ReviewRestoredHijacksAreNotForeign(t *testing.T) {
	f, _, d, previous := r09Seed(t)
	settings := r09Base("restore").Settings
	off := settings
	off.Enabled = false
	p := r09Plan(t, d, off, previous)
	first := r09ReviewOwned(t, previous, "bypass:192.0.2.2/32")
	f.ApplyExternal(routerfake.ExternalWrite{Faults: []routerfake.Fault{{Method: "DELETE", Path: ros.PathAddresses + "/" + first.ID, Status: 400, Body: routerfake.Row{"message": "injected refusal"}}}})
	if _, err := dnsguard.Apply(context.Background(), d.Client, *p.Ops[0].(*dnsguard.Replace)); err == nil || device.RouterErrorKind(err) == device.KindRollback {
		t.Fatalf("refused withdrawal should roll back completely: %v", err)
	}
	manifest := ros.GuardRaw(d.DNSGuardManifest())
	for _, role := range []string{"hijack-udp", "hijack-tcp", "netwatch"} {
		if r09ReviewOwned(t, manifest, role).ID == r09ReviewOwned(t, previous, role).ID {
			t.Fatalf("%s was not re-created by the rollback", role)
		}
	}
	next := r09Plan(t, d, settings, manifest)
	if next.Status != "synced" || len(next.Blockers) != 0 || slices.ContainsFunc(next.Blockers, func(s string) bool { return strings.Contains(s, dnsguard.HijackComment) }) {
		t.Fatalf("restored objects not owned: %s", r09Raw(next))
	}
}
