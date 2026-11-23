package fixedip

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"configflow/internal/lifecycle"
)

const fixedROSLeases = "/ip/dhcp-server/lease"

func (d rosDevice) fixedLease(ctx context.Context, id string) (ros.Row, error) {
	if err := fixedROSID(id); err != nil {
		return nil, err
	}
	value, err := d.Get(ctx, fixedROSLeases+"/"+ros.EscapePathID(id), ros.Timeout)
	if device.RouterErrorKind(err) == device.KindMissingResource {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row := ros.AsRow(value)
	if row == nil || row.ID() != id {
		return nil, device.Error(messageFixedROSReadID)
	}
	return row, nil
}
func (d rosDevice) removeFixedLease(ctx context.Context, b *Binding, expected ros.Row) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	row, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	if !fixedROSMatches(row, b) || expected != nil && !ros.SameRow(fixedROSFields(row), expected) {
		return device.Error(messageFixedROSExternal)
	}
	_, failure := d.Request(ctx, "DELETE", fixedROSLeases+"/"+ros.EscapePathID(b.ID), nil, ros.Timeout)
	if failure != nil && device.RouterErrorKind(failure) != device.KindUncertainWrite {
		return failure
	}
	remaining, readError := d.fixedLease(ctx, b.ID)
	if readError != nil {
		remaining, err = d.fixedLease(ctx, b.ID)
		if err != nil {
			return device.RollbackError(fmt.Sprintf(messageFixedROSDeleteUnknown, err))
		}
	}
	if remaining != nil {
		if failure != nil {
			return failure
		}
		if readError != nil {
			return readError
		}
		return device.Error(messageFixedROSStillPresent)
	}
	return nil
}

// ApplyFixedIP detaches a client disconnect, rechecks the preview, and retains
// exact proof for compensation. Runtime shutdown still cancels every device I/O.
func (d rosDevice) ApplyFixedIP(ctx context.Context, op *Operation) (binding *Binding, err error) {
	ctx = lifecycle.Detach(ctx)
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	latest, err := d.PreviewFixedIP(ctx, op.IP, op.MAC, fixedText(op.Name))
	if err != nil {
		return nil, err
	}
	if !device.JSONEqual(latest, op) {
		return nil, device.Error(messageFixedROSPreviewChanged)
	}
	expected := fixedOperationBinding(op, "routeros")
	nonce, err := fixedNonce()
	if err != nil {
		return nil, err
	}
	expected.Comment += ":" + nonce
	original := maps.Clone(op.Expected)
	var acquired *Binding
	var acquiredFields ros.Row
	conversionPending := false
	defer func() {
		if err == nil || ctx.Err() != nil {
			return
		}
		if acquired == nil && !conversionPending {
			return
		}
		target := acquired
		if target == nil {
			target = expected
		}
		current, e := d.fixedLease(ctx, target.ID)
		if e == nil && current != nil && !current.Flag("dynamic") {
			fields := maps.Clone(acquiredFields)
			if fields == nil {
				fields = maps.Clone(original)
			}
			if fields == nil {
				e = device.Error(messageFixedRollbackChanged)
			} else {
				comment := fields.Text("comment")
				fields["comment"] = current.Text("comment")
				if current.Text("address") != expected.IP || fixedMAC(current.Text("mac-address")) != expected.MAC || current.Text("server") != fixedText(expected.Server) || (current.Text("comment") != comment && current.Text("comment") != expected.Comment) || !ros.SameRow(fixedROSFields(current), fields) {
					e = device.Error(messageFixedRollbackChanged)
				} else {
					var b *Binding
					b, e = fixedROSBinding(current)
					if e == nil {
						e = d.removeFixedLease(ctx, b, fields)
					}
					if e == nil && op.Kind == "make_static" {
						err = device.Error(err.Error() + "；" + messageFixedDynamicUndo)
					}
				}
			}
		}
		if e != nil {
			err = device.RollbackError(fmt.Sprintf(messageFixedROSRollback, err, e))
		}
	}()
	if op.Kind == "make_static" {
		_, err = d.Request(ctx, "POST", fixedROSLeases+"/make-static", ros.Row{".id": fixedText(op.ID)}, ros.Timeout)
		if err != nil {
			if device.RouterErrorKind(err) == device.KindUncertainWrite {
				conversionPending = true
				row, e := d.fixedLease(ctx, fixedText(op.ID))
				if e != nil {
					return nil, e
				}
				if row != nil && !row.Flag("dynamic") {
					acquired, e = fixedROSBinding(row)
					if e != nil {
						return nil, e
					}
					acquiredFields = maps.Clone(original)
				}
			}
			return nil, err
		}
		conversionPending = true
		row, e := d.fixedLease(ctx, fixedText(op.ID))
		if e != nil {
			return nil, e
		}
		if row == nil || row.Flag("dynamic") {
			return nil, device.Error(messageFixedROSNotStatic)
		}
		acquired, e = fixedROSBinding(row)
		if e != nil {
			return nil, e
		}
		acquiredFields = maps.Clone(original)
		if !ros.SameRow(fixedROSFields(row), original) {
			return nil, device.RollbackError(messageFixedROSConversionChanged)
		}
		if acquired.IP != expected.IP || acquired.MAC != expected.MAC || fixedText(acquired.Server) != fixedText(expected.Server) {
			acquired = nil
			return nil, device.RollbackError(messageFixedROSConversionTarget)
		}
		if _, err = d.Request(ctx, "PATCH", fixedROSLeases+"/"+ros.EscapePathID(expected.ID), ros.Row{"comment": expected.Comment}, ros.Timeout); err != nil {
			return nil, err
		}
	} else {
		rows, e := d.List(ctx, fixedROSLeases)
		if e != nil {
			return nil, e
		}
		known := ros.RowIDs(rows)
		values := ros.Row{"mac-address": op.MAC, "address": op.IP, "server": fixedText(op.Server), "comment": expected.Comment, "disabled": "false"}
		var response json.RawMessage
		response, err = d.Request(ctx, "PUT", fixedROSLeases, values, ros.Timeout)
		row := ros.AsRow(response)
		newID := ""
		if row != nil {
			newID = row.ID()
		}
		if err == nil && (!ros.IDPattern.MatchString(newID) || known[newID]) {
			err = device.KindError(device.KindUncertainWrite, messageFixedROSNewID)
		}
		if err != nil {
			if device.RouterErrorKind(err) != device.KindUncertainWrite {
				return nil, err
			}
			cause := err
			rows, e = d.List(ctx, fixedROSLeases)
			if e != nil {
				return nil, device.RollbackError(fmt.Sprintf(messageFixedKeepScene, cause, e))
			}
			matches := []ros.Row{}
			for _, r := range rows {
				candidate := *expected
				candidate.ID = r.ID()
				if !known[r.ID()] && fixedROSMatches(r, &candidate) {
					matches = append(matches, r)
				}
			}
			if len(matches) != 1 {
				return nil, device.RollbackError(fmt.Sprintf(messageFixedKeepScene, cause, messageFixedROSNotUnique))
			}
			recovered, e := d.fixedLease(ctx, matches[0].ID())
			if e != nil {
				return nil, device.RollbackError(fmt.Sprintf(messageFixedKeepScene, cause, e))
			}
			if recovered == nil || !ros.SameRow(fixedROSFields(recovered), fixedROSFields(matches[0])) {
				return nil, device.RollbackError(fmt.Sprintf(messageFixedKeepScene, cause, messageFixedROSRecoveryChanged))
			}
			acquired, e = fixedROSBinding(recovered)
			if e != nil {
				return nil, e
			}
			acquiredFields = fixedROSFields(recovered)
			expected.ID = acquired.ID
			return nil, cause
		}
		expected.ID = newID
		copy := *expected
		acquired = &copy
		fields := maps.Clone(values)
		maps.Copy(fields, row)
		acquiredFields = fixedROSFields(fields)
	}
	row, err := d.fixedLease(ctx, expected.ID)
	if err != nil {
		return nil, err
	}
	if err = fixedROSCheck(row, expected, false); err != nil {
		return nil, err
	}
	expected.Undo = &Undo{Kind: op.Kind, Expected: fixedROSFields(row), Original: original}
	return expected, nil
}

// RollbackFixedIP removes only the proven reservation created by this job.
func (d rosDevice) RollbackFixedIP(ctx context.Context, b *Binding) error {
	ctx = lifecycle.Detach(ctx)
	if b == nil || !strings.HasPrefix(b.Comment, fixedIPPrefix) || b.Undo == nil || b.Undo.Expected == nil {
		return device.Error(messageFixedROSProof)
	}
	return d.removeFixedLease(ctx, b, b.Undo.Expected)
}

// PreviewDeleteFixedIP captures the complete stable before-image for deletion.
func (d rosDevice) PreviewDeleteFixedIP(ctx context.Context, b *Binding) (*Operation, error) {
	if b == nil || b.Vendor != "routeros" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, device.Error(messageFixedROSDeleteOwn)
	}
	row, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if !fixedROSMatches(row, b) {
		return nil, device.Error(messageFixedROSDeleteChanged)
	}
	return fixedDeleteOperation(b, fixedROSFields(row), fmt.Sprintf(messageFixedROSDeleteSummary, b.IP, b.MAC)), nil
}

// DeleteFixedIP re-proves the approved lease and reconciles lost delete replies.
func (d rosDevice) DeleteFixedIP(ctx context.Context, op *Operation) (*Binding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if op == nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	b := fixedOperationBinding(op, "routeros")
	latest, err := d.PreviewDeleteFixedIP(ctx, b)
	if err != nil {
		return nil, err
	}
	if !device.JSONEqual(latest, op) {
		return nil, device.Error(messageFixedROSDeletePreview)
	}
	if err = d.removeFixedLease(ctx, b, op.Expected); err != nil {
		return nil, err
	}
	b.Undo = &Undo{Kind: "delete", Expected: maps.Clone(op.Expected)}
	return b, nil
}

// RestoreDeletedFixedIP refuses ID reuse and conflicting IP/MAC allocations.
func (d rosDevice) RestoreDeletedFixedIP(ctx context.Context, b *Binding) (*Binding, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b == nil || b.Undo == nil || b.Undo.Expected == nil || b.Undo.Kind != "delete" || !strings.HasPrefix(b.Comment, fixedIPPrefix) {
		return nil, device.Error(messageFixedROSRestoreProof)
	}
	current, err := d.fixedLease(ctx, b.ID)
	if err != nil {
		return nil, err
	}
	if current != nil {
		if fixedROSMatches(current, b) && ros.SameRow(fixedROSFields(current), b.Undo.Expected) {
			return fixedROSBinding(current)
		}
		return nil, device.Error(messageFixedROSRestoreID)
	}
	rows, err := d.List(ctx, fixedROSLeases)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.Text("address") == b.IP || fixedMAC(row.Text("mac-address")) == b.MAC {
			return nil, device.Error(messageFixedROSRestoreConflict)
		}
	}
	value, err := d.Request(ctx, "PUT", fixedROSLeases, b.Undo.Expected, ros.Timeout)
	if err != nil {
		return nil, err
	}
	row := ros.AsRow(value)
	if row == nil {
		return nil, device.Error(messageFixedROSID)
	}
	if err = fixedROSID(row.ID()); err != nil {
		return nil, err
	}
	restored := *b
	restored.ID = row.ID()
	actual, err := d.fixedLease(ctx, restored.ID)
	if err != nil {
		return nil, err
	}
	if err = fixedROSCheck(actual, &restored, b.Undo.Expected.Flag("disabled")); err != nil {
		return nil, err
	}
	if !ros.SameRow(fixedROSFields(actual), b.Undo.Expected) {
		return nil, device.RollbackError(messageFixedROSRestoreCheck)
	}
	return fixedROSBinding(actual)
}
