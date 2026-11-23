// Package rospolicy implements RouterOS source-based policy routing and NAT
// address-list synchronization: planning, ownership verification, and execution.
package rospolicy

import (
	"context"
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
)

// SettingsPath is the RouterOS routing settings REST path.
const SettingsPath = rosSettings

// Paths returns a copy of the RouterOS paths inspected by policy routing.
func Paths() []string { return append([]string{}, policyPaths...) }

// StateReader reads the router's static routes and NAT context for policy
// verification and replacement checks.
type StateReader func(context.Context) (*device.State, *ros.NatContext, error)

type rosDevice struct {
	*ros.Client
	readState StateReader
}

func (d rosDevice) applyOp(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := jsondecode.Unmarshal(raw, &head); err != nil {
		return nil, device.Error(device.MessageOperationFormat)
	}
	switch head.Kind {
	case "nat_list_sync":
		var op NATListSync
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		undo, err := d.ApplyNATLists(ctx, op)
		if err != nil {
			return nil, err
		}
		return json.Marshal(undo)
	case "nat_list_restore":
		var op NATListUndo
		if jsondecode.Unmarshal(raw, &op) != nil {
			return nil, device.Error(device.MessageOperationFormat)
		}
		if err := d.RestoreNATLists(ctx, op); err != nil {
			return nil, err
		}
		return json.RawMessage(`{}`), nil
	default:
		return d.ApplyBase(ctx, raw)
	}
}

func (d rosDevice) applyMany(ctx context.Context, ops []json.RawMessage, record func(json.RawMessage, json.RawMessage)) error {
	return d.ApplyManyWith(ctx, ops, record, d.applyOp)
}

func (d rosDevice) restoreMany(ctx context.Context, undos []json.RawMessage, stopOnFailure bool) []string {
	return d.RestoreManyWith(ctx, undos, stopOnFailure, d.applyOp)
}

// StateNATLists inspects NAT address-list membership and references on c.
func StateNATLists(ctx context.Context, c *ros.Client, nats []ros.Row, byName map[string][]ros.Row, mangle []ros.Row) (*NATLists, error) {
	return (rosDevice{Client: c}).stateNATLists(ctx, nats, byName, mangle)
}

// NATResources reads the currently managed NAT address-list baseline from c.
func NATResources(ctx context.Context, c *ros.Client) (*NATBaseline, error) {
	return (rosDevice{Client: c}).NATResources(ctx)
}

// ApplyNATLists executes one NAT address-list sync delta on c.
func ApplyNATLists(ctx context.Context, c *ros.Client, op NATListSync) (*NATListUndo, error) {
	return (rosDevice{Client: c}).ApplyNATLists(ctx, op)
}

// RestoreNATLists replays a NAT address-list undo journal on c.
func RestoreNATLists(ctx context.Context, c *ros.Client, op NATListUndo) error {
	return (rosDevice{Client: c}).RestoreNATLists(ctx, op)
}

// DeleteNATRules deletes managed NAT rules and appends compensation steps.
func DeleteNATRules(ctx context.Context, c *ros.Client, items []ros.Resource, journal *[]ros.NATStep) error {
	return (rosDevice{Client: c}).DeleteNATRules(ctx, items, journal)
}

// CreateResource creates one managed RouterOS resource and verifies readback.
func CreateResource(ctx context.Context, c *ros.Client, path string, values ros.Row, journal *[]ros.NATStep, ids ros.ROSIDs, restoring bool) (ros.Resource, error) {
	return (rosDevice{Client: c}).createResource(ctx, path, values, journal, ids, restoring)
}

// DeleteResource deletes one managed RouterOS resource after proving ownership.
func DeleteResource(ctx context.Context, c *ros.Client, item ros.Resource, journal *[]ros.NATStep) error {
	return (rosDevice{Client: c}).deleteResource(ctx, item, journal)
}

// SnapshotLive reads the policy-routing tables from c.
func SnapshotLive(ctx context.Context, c *ros.Client, reserve bool) (*ROSPolicySnapshot, error) {
	return (rosDevice{Client: c}).snapshotPolicy(ctx, reserve)
}

// ApplyROSPolicy applies a typed policy-routing replacement plan on c.
func ApplyROSPolicy(ctx context.Context, c *ros.Client, readState StateReader, op *ROSPolicyReplace) (*ROSPolicyUndo, error) {
	return (rosDevice{Client: c, readState: readState}).ApplyROSPolicy(ctx, op)
}

// ApplyPolicyWrite applies a decoded wire policyWrite operation on c.
func ApplyPolicyWrite(ctx context.Context, c *ros.Client, readState StateReader, op PolicyWrite) (*ROSPolicyUndo, error) {
	return (rosDevice{Client: c, readState: readState}).applyPolicy(ctx, op)
}

// RestoreROSPolicy replays a policy-routing undo journal on c.
func RestoreROSPolicy(ctx context.Context, c *ros.Client, undo ROSPolicyUndo) error {
	return (rosDevice{Client: c}).RestoreROSPolicy(ctx, undo)
}
