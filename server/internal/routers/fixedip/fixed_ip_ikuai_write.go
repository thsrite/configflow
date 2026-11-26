package fixedip

import (
	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"context"
	"fmt"
	"maps"
	"strings"

	"configflow/internal/lifecycle"
)

func (d ikuaiDevice) fixedAllocation(ctx context.Context, id string) (ikuai.Row, error) {
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
func fixedIKExpected(payload ros.Row) ros.Row {
	out := maps.Clone(payload)
	for _, key := range []string{"id", "termname", "ip_addr_int", "hostname"} {
		delete(out, key)
	}
	return out
}
func (d ikuaiDevice) fixedNewAllocation(ctx context.Context, known map[string]bool, payload ros.Row) (ikuai.Row, error) {
	rows, err := d.fixedStatic(ctx)
	if err != nil {
		return nil, err
	}
	matches := []ikuai.Row{}
	for _, row := range rows {
		if row.Is("tagname", payload.Text("tagname")) {
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
	if !ros.SameRow(fields, fixedIKExpected(payload)) {
		return nil, nil
	}
	return matches[0], nil
}
func (d ikuaiDevice) removeFixedAllocation(ctx context.Context, b *Binding, expected ros.Row) error {
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
	if !strings.HasPrefix(b.Comment, fixedIPPrefix) || !fixedIKMatches(current, b) || !ros.SameRow(fields, expected) {
		return device.KindError(device.KindConflict, messageFixedIKExternal)
	}
	if _, err = d.WriteForm(ctx, "dhcp_static", "del", ikuai.Row{"id": b.ID}, 0); err != nil {
		if device.RouterErrorKind(err) == device.KindRejected {
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
		return device.Error(messageFixedIKStillPresent)
	}
	return nil
}

// ApplyFixedIP uses the firmware's unique short tag and full static_ view to
// prove a new allocation, including the intentionally inherited DNS settings.
func (d ikuaiDevice) ApplyFixedIP(ctx context.Context, op *Operation) (binding *Binding, err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	preview, err := d.PreviewFixedIP(ctx, op.IP, op.MAC, fixedText(op.Name))
	if err != nil {
		return nil, err
	}
	if !device.JSONEqual(preview, op) {
		return nil, device.KindError(device.KindConflict, messageFixedIKPreview)
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
	payload := ros.Row{"interface": scope.Interface, "gateway": scope.Gateway, "ip_addr": op.IP, "mac": op.MAC, "dns1": "", "dns2": "", "enabled": "yes", "hostname": "", "comment": op.Comment, "tagname": "cfip_" + nonce[:10]}
	var acquired ikuai.Row
	defer func() {
		if err == nil || ctx.Err() != nil || device.RouterErrorKind(err) == device.KindRejected {
			return
		}
		var e error
		if acquired == nil {
			acquired, e = d.fixedNewAllocation(ctx, known, payload)
		}
		if e == nil && acquired != nil {
			e = d.removeFixedAllocation(ctx, fixedIKBinding(acquired), fixedIKExpected(payload))
		} else if e == nil {
			var rows []ikuai.Row
			rows, e = d.fixedStatic(ctx)
			if e == nil {
				for _, row := range rows {
					if row.Is("tagname", payload.Text("tagname")) {
						e = device.Error(messageFixedIKChanged)
						break
					}
				}
			}
		}
		if e != nil {
			err = device.RollbackError(fmt.Sprintf(messageFixedIKRollback, err, e))
		}
	}()
	if _, err = d.WriteForm(ctx, "dhcp_static", "add", payload, 0); err != nil {
		return nil, err
	}
	acquired, err = d.fixedNewAllocation(ctx, known, payload)
	if err != nil {
		return nil, err
	}
	if acquired == nil {
		return nil, device.Error(messageFixedIKNewCheck)
	}
	actual, err := d.fixedAllocation(ctx, fixedIKIDText(acquired["id"]))
	if err != nil {
		return nil, err
	}
	if actual == nil {
		return nil, device.Error(messageFixedIKIDCheck)
	}
	fields, err := fixedIKFields(actual)
	if err != nil {
		return nil, err
	}
	if !ros.SameRow(fields, fixedIKExpected(payload)) {
		return nil, device.Error(messageFixedIKIDCheck)
	}
	binding = fixedIKBinding(actual)
	binding.Undo = &Undo{Expected: fixedIKExpected(payload)}
	return binding, nil
}

// RollbackFixedIP deletes only the unchanged allocation proved by this job.
func (d ikuaiDevice) RollbackFixedIP(ctx context.Context, b *Binding) error {
	ctx = lifecycle.Detach(ctx)
	if b == nil || b.Undo == nil || b.Undo.Expected == nil {
		return device.Error(messageFixedIKProof)
	}
	return d.removeFixedAllocation(ctx, b, b.Undo.Expected)
}

// PreviewDeleteFixedIP never adopts a foreign or changed allocation.
func (d ikuaiDevice) PreviewDeleteFixedIP(ctx context.Context, b *Binding) (*Operation, error) {
	if b == nil || b.Vendor != "ikuai" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, device.KindError(device.KindConflict, messageFixedIKDeleteOwn)
	}
	current, err := d.fixedAllocation(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if !fixedIKMatches(current, b) {
		return nil, device.KindError(device.KindConflict, messageFixedIKDeleteChanged)
	}
	expected, err := fixedIKFields(current)
	if err != nil {
		return nil, err
	}
	return fixedDeleteOperation(fixedIKBinding(current), expected, fmt.Sprintf(messageFixedIKDeleteSummary, fixedText(b.Interface), b.IP, b.MAC)), nil
}

// DeleteFixedIP reconciles an uncertain delete before allowing metadata removal.
func (d ikuaiDevice) DeleteFixedIP(ctx context.Context, op *Operation) (*Binding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	binding := fixedOperationBinding(op, "ikuai")
	latest, err := d.PreviewDeleteFixedIP(ctx, binding)
	if err != nil {
		return nil, err
	}
	if !device.JSONEqual(latest, op) {
		return nil, device.KindError(device.KindConflict, messageFixedIKDeletePreview)
	}
	if err = d.removeFixedAllocation(ctx, binding, op.Expected); err != nil {
		if device.RouterErrorKind(err) == device.KindRejected || ctx.Err() != nil {
			return nil, err
		}
		remaining, readErr := d.fixedAllocation(ctx, binding.ID)
		if readErr != nil {
			return nil, device.RollbackError(fmt.Sprintf(messageFixedIKDeleteUnknown, err, readErr))
		}
		if remaining != nil {
			return nil, err
		}
	}
	binding.Undo = &Undo{Kind: "delete", Expected: maps.Clone(op.Expected)}
	return binding, nil
}

// RestoreDeletedFixedIP rejects reused IDs and competing IP, MAC or tag claims.
func (d ikuaiDevice) RestoreDeletedFixedIP(ctx context.Context, b *Binding) (*Binding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.Undo == nil || b.Undo.Expected == nil || b.Undo.Kind != "delete" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, device.Error(messageFixedIKRestoreProof)
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
			if fixedIKMatches(row, b) && ros.SameRow(fields, b.Undo.Expected) {
				return fixedIKBinding(row), nil
			}
			return nil, device.Error(messageFixedIKRestoreID)
		}
	}
	for _, row := range rows {
		if row.Is("ip_addr", b.IP) || fixedMAC(row.Text("mac")) == b.MAC || row.Is("tagname", b.Undo.Expected.Text("tagname")) {
			return nil, device.Error(messageFixedIKRestoreConflict)
		}
	}
	payload := maps.Clone(b.Undo.Expected)
	payload["hostname"] = ""
	_, err = d.WriteForm(ctx, "dhcp_static", "add", payload, 0)
	if device.RouterErrorKind(err) == device.KindRejected || ctx.Err() != nil {
		return nil, err
	}
	restored, readErr := d.fixedNewAllocation(ctx, known, b.Undo.Expected)
	if readErr != nil {
		return nil, readErr
	}
	if restored != nil {
		return fixedIKBinding(restored), nil
	}
	if err != nil {
		return nil, err
	}
	return nil, device.RollbackError(messageFixedIKRestoreCheck)
}
