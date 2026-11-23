package routers

import (
	"configflow/internal/jsondecode"
	"context"
	"fmt"
	"maps"
	"strings"

	"configflow/internal/lifecycle"
)

func (d *Ikuai) fixedAllocation(ctx context.Context, id string) (ikuaiRow, error) {
	rows, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if fixedIKIDText(row["id"]) == id {
			return row, nil
		}
	}
	return nil, nil
}
func fixedIKExpected(payload ikuaiRow) ikuaiRow {
	out := maps.Clone(payload)
	for _, key := range []string{"id", "termname", "ip_addr_int", "hostname"} {
		delete(out, key)
	}
	return out
}
func (d *Ikuai) fixedNewAllocation(ctx context.Context, known map[string]bool, payload ikuaiRow) (ikuaiRow, error) {
	rows, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	matches := []ikuaiRow{}
	for _, row := range rows {
		if row.is("tagname", payload.text("tagname")) {
			matches = append(matches, row)
		}
	}
	if len(matches) != 1 || known[fixedIKIDText(matches[0]["id"])] {
		return nil, nil
	}
	fields, err := fixedIKFields(matches[0])
	if err != nil {
		return nil, err
	}
	if !sameRow(Row(fields), Row(fixedIKExpected(payload))) {
		return nil, nil
	}
	return matches[0], nil
}
func (d *Ikuai) removeFixedAllocation(ctx context.Context, b *FixedIPBinding, expected ikuaiRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := d.fixedAllocation(ctx, b.ID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	fields, err := fixedIKFields(current)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(b.Comment, fixedIPPrefix) || !fixedIKMatches(current, b) || !sameRow(Row(fields), Row(expected)) {
		return rosError(KindConflict, messageFixedIKExternal)
	}
	if _, err = d.writeForm(ctx, "dhcp_static", "del", ikuaiRow{"id": b.ID}, 0); err != nil {
		if routerErrorKind(err) == KindRejected {
			return err
		}
		remaining, readErr := d.fixedAllocation(ctx, b.ID)
		if readErr != nil {
			return readErr
		}
		if remaining == nil {
			return nil
		}
		return err
	}
	remaining, err := d.fixedAllocation(ctx, b.ID)
	if err != nil {
		return err
	}
	if remaining != nil {
		return routerError(messageFixedIKStillPresent)
	}
	return nil
}

// ApplyFixedIP uses the firmware's unique short tag and full static_ view to
// prove a new allocation, including the intentionally inherited DNS settings.
func (d *Ikuai) ApplyFixedIP(ctx context.Context, op *FixedIPOperation) (binding *FixedIPBinding, err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, routerError(messageOperationFormat)
	}
	preview, err := d.PreviewFixedIP(ctx, op.IP, op.MAC, fixedText(op.Name))
	if err != nil {
		return nil, err
	}
	if !jsonEqual(preview, op) {
		return nil, rosError(KindConflict, messageFixedIKPreview)
	}
	rows, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, row := range rows {
		known[fixedIKIDText(row["id"])] = true
	}
	var scope fixedIKScope
	if err = jsondecode.Unmarshal(op.Scope, &scope); err != nil {
		return nil, err
	}
	nonce, err := fixedNonce()
	if err != nil {
		return nil, err
	}
	payload := ikuaiRow{"interface": scope.Interface, "gateway": scope.Gateway, "ip_addr": op.IP, "mac": op.MAC, "dns1": "", "dns2": "", "enabled": "yes", "hostname": "", "comment": op.Comment, "tagname": "cfip_" + nonce[:10]}
	var acquired ikuaiRow
	defer func() {
		if err == nil || ctx.Err() != nil || routerErrorKind(err) == KindRejected {
			return
		}
		var e error
		if acquired == nil {
			acquired, e = d.fixedNewAllocation(ctx, known, payload)
		}
		if e == nil && acquired != nil {
			e = d.removeFixedAllocation(ctx, fixedIKBinding(acquired), fixedIKExpected(payload))
		} else if e == nil {
			var rows []ikuaiRow
			rows, e = d.fixedStatic(ctx)
			if e == nil {
				for _, row := range rows {
					if row.is("tagname", payload.text("tagname")) {
						e = routerError(messageFixedIKChanged)
						break
					}
				}
			}
		}
		if e != nil {
			err = rollbackError(fmt.Sprintf(messageFixedIKRollback, err, e))
		}
	}()
	if _, err = d.writeForm(ctx, "dhcp_static", "add", payload, 0); err != nil {
		return nil, err
	}
	acquired, err = d.fixedNewAllocation(ctx, known, payload)
	if err != nil {
		return nil, err
	}
	if acquired == nil {
		return nil, routerError(messageFixedIKNewCheck)
	}
	actual, err := d.fixedAllocation(ctx, fixedIKIDText(acquired["id"]))
	if err != nil {
		return nil, err
	}
	if actual == nil {
		return nil, routerError(messageFixedIKIDCheck)
	}
	fields, err := fixedIKFields(actual)
	if err != nil {
		return nil, err
	}
	if !sameRow(Row(fields), Row(fixedIKExpected(payload))) {
		return nil, routerError(messageFixedIKIDCheck)
	}
	binding = fixedIKBinding(actual)
	binding.Undo = &FixedIPUndo{Expected: Row(fixedIKExpected(payload))}
	return binding, nil
}

// RollbackFixedIP deletes only the unchanged allocation proved by this job.
func (d *Ikuai) RollbackFixedIP(ctx context.Context, b *FixedIPBinding) error {
	ctx = lifecycle.Detach(ctx)
	if b == nil || b.Undo == nil || b.Undo.Expected == nil {
		return routerError(messageFixedIKProof)
	}
	return d.removeFixedAllocation(ctx, b, ikuaiRow(b.Undo.Expected))
}

// PreviewDeleteFixedIP never adopts a foreign or changed allocation.
func (d *Ikuai) PreviewDeleteFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPOperation, error) {
	if b == nil || b.Vendor != "ikuai" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, rosError(KindConflict, messageFixedIKDeleteOwn)
	}
	current, err := d.fixedAllocation(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if !fixedIKMatches(current, b) {
		return nil, rosError(KindConflict, messageFixedIKDeleteChanged)
	}
	expected, err := fixedIKFields(current)
	if err != nil {
		return nil, err
	}
	return fixedDeleteOperation(fixedIKBinding(current), Row(expected), fmt.Sprintf(messageFixedIKDeleteSummary, fixedText(b.Interface), b.IP, b.MAC)), nil
}

// DeleteFixedIP reconciles an uncertain delete before allowing metadata removal.
func (d *Ikuai) DeleteFixedIP(ctx context.Context, op *FixedIPOperation) (*FixedIPBinding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, routerError(messageOperationFormat)
	}
	binding := fixedOperationBinding(op, "ikuai")
	latest, err := d.PreviewDeleteFixedIP(ctx, binding)
	if err != nil {
		return nil, err
	}
	if !jsonEqual(latest, op) {
		return nil, rosError(KindConflict, messageFixedIKDeletePreview)
	}
	if err = d.removeFixedAllocation(ctx, binding, ikuaiRow(op.Expected)); err != nil {
		if routerErrorKind(err) == KindRejected || ctx.Err() != nil {
			return nil, err
		}
		remaining, readErr := d.fixedAllocation(ctx, binding.ID)
		if readErr != nil {
			return nil, rollbackError(fmt.Sprintf(messageFixedIKDeleteUnknown, err, readErr))
		}
		if remaining != nil {
			return nil, err
		}
	}
	binding.Undo = &FixedIPUndo{Kind: "delete", Expected: maps.Clone(op.Expected)}
	return binding, nil
}

// RestoreDeletedFixedIP rejects reused IDs and competing IP, MAC or tag claims.
func (d *Ikuai) RestoreDeletedFixedIP(ctx context.Context, b *FixedIPBinding) (*FixedIPBinding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.Undo == nil || b.Undo.Expected == nil || b.Undo.Kind != "delete" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, routerError(messageFixedIKRestoreProof)
	}
	rows, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, row := range rows {
		id := fixedIKIDText(row["id"])
		known[id] = true
		if id == b.ID {
			fields, e := fixedIKFields(row)
			if e != nil {
				return nil, e
			}
			if fixedIKMatches(row, b) && sameRow(Row(fields), b.Undo.Expected) {
				return fixedIKBinding(row), nil
			}
			return nil, routerError(messageFixedIKRestoreID)
		}
	}
	for _, row := range rows {
		if row.is("ip_addr", b.IP) || fixedMAC(row.text("mac")) == b.MAC || row.is("tagname", b.Undo.Expected.text("tagname")) {
			return nil, routerError(messageFixedIKRestoreConflict)
		}
	}
	payload := ikuaiRow(maps.Clone(b.Undo.Expected))
	payload["hostname"] = ""
	_, err = d.writeForm(ctx, "dhcp_static", "add", payload, 0)
	if routerErrorKind(err) == KindRejected || ctx.Err() != nil {
		return nil, err
	}
	restored, readErr := d.fixedNewAllocation(ctx, known, ikuaiRow(b.Undo.Expected))
	if readErr != nil {
		return nil, readErr
	}
	if restored != nil {
		return fixedIKBinding(restored), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, rollbackError(messageFixedIKRestoreCheck)
}
