package routers

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"configflow/internal/routerdoc"
)

// FixedIPBinding is the portable ownership record. Undo is transient and is
// removed before persistence; optional vendor fields retain their presence.
type FixedIPBinding struct {
	RouterID  string       `json:"router_id,omitempty"`
	Vendor    string       `json:"vendor"`
	ID        string       `json:"id"`
	IP        string       `json:"ip"`
	MAC       string       `json:"mac"`
	Comment   string       `json:"comment"`
	Server    *string      `json:"server,omitempty"`
	Interface *string      `json:"interface,omitempty"`
	Gateway   *string      `json:"gateway,omitempty"`
	Undo      *FixedIPUndo `json:"undo,omitempty"`
}

// FixedIPUndo proves the stable fields written by a single reservation job.
type FixedIPUndo struct {
	Kind     string `json:"kind,omitempty"`
	Expected Row    `json:"expected"`
	Original Row    `json:"original,omitempty"`
}

// FixedIPOperation is immutable preview evidence, never trusted without rebuilding.
type FixedIPOperation struct {
	Kind      string          `json:"kind"`
	IP        string          `json:"ip"`
	MAC       string          `json:"mac"`
	Name      *string         `json:"name,omitempty"`
	Vendor    string          `json:"vendor,omitempty"`
	ID        *string         `json:"id,omitempty"`
	Comment   string          `json:"comment"`
	Server    *string         `json:"server,omitempty"`
	Interface *string         `json:"interface,omitempty"`
	Gateway   *string         `json:"gateway,omitempty"`
	Scope     json.RawMessage `json:"scope,omitempty"`
	Expected  Row             `json:"expected,omitempty"`
	Summary   string          `json:"summary"`
}

// MarshalJSON retains RouterOS add's explicit null expected field.
func (o FixedIPOperation) MarshalJSON() ([]byte, error) {
	type plain FixedIPOperation
	raw, err := json.Marshal(plain(o))
	if err != nil {
		return nil, err
	}
	if o.ID != nil && o.Kind != "delete" {
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		fields["expected"] = device.JSON(o.Expected)
		return json.Marshal(fields)
	}
	return raw, nil
}

// FixedIPSnapshot is a complete reservation snapshot. A malformed write table
// is rejected; read-only discovery's row-isolation rules do not grant ownership.
type FixedIPSnapshot struct {
	Vendor string `json:"-"`
	// RouterOS tables.
	Servers   []Row `json:"servers"`
	Networks  []Row `json:"networks,omitempty"`
	Addresses []Row `json:"addresses,omitempty"`
	Leases    []Row `json:"leases"`
	// iKuai tables.
	ikuaiServers []ikuai.Row
	ikuaiStatic  []ikuai.Row
	ikuaiLeases  []ikuai.Row
}

// FixedIPState separates control eligibility from evidence about existing leases.
type FixedIPState struct {
	Eligible bool            `json:"eligible"`
	Reason   string          `json:"reason"`
	Fixed    bool            `json:"fixed"`
	Owned    bool            `json:"owned"`
	Dynamic  bool            `json:"dynamic"`
	Scope    json.RawMessage `json:"scope"`
	Lease    Row             `json:"lease"`
	Binding  *FixedIPBinding `json:"binding,omitempty"`
}

// FixedIPDriver implements ownership-safe DHCP reservation writes and compensation.
type FixedIPDriver interface {
	Driver
	ReadFixedIP(context.Context) (*FixedIPSnapshot, error)
	PreviewFixedIP(context.Context, string, string, string) (*FixedIPOperation, error)
	ApplyFixedIP(context.Context, *FixedIPOperation) (*FixedIPBinding, error)
	RollbackFixedIP(context.Context, *FixedIPBinding) error
	PreviewDeleteFixedIP(context.Context, *FixedIPBinding) (*FixedIPOperation, error)
	DeleteFixedIP(context.Context, *FixedIPOperation) (*FixedIPBinding, error)
	RestoreDeletedFixedIP(context.Context, *FixedIPBinding) (*FixedIPBinding, error)
	SeenFixedIP(context.Context, string, string) (bool, error)
}

const fixedIPPrefix = "cf-client:"

func fixedMAC(value string) string {
	mac := routerdoc.NormalizeMAC(value)
	if len(mac) != 17 {
		return ""
	}
	bytes, err := hex.DecodeString(strings.ReplaceAll(mac, ":", ""))
	if err != nil || bytes[0]&1 != 0 || mac == "00:00:00:00:00:00" {
		return ""
	}
	return mac
}
func fixedNonce() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("create reservation identity: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
func fixedPointer(value string) *string { return &value }
func fixedText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func fixedOperationBinding(op *FixedIPOperation, vendor string) *FixedIPBinding {
	return &FixedIPBinding{Vendor: vendor, ID: fixedText(op.ID), IP: op.IP, MAC: op.MAC, Comment: op.Comment, Server: op.Server, Interface: op.Interface, Gateway: op.Gateway}
}
func fixedDeleteOperation(b *FixedIPBinding, expected Row, summary string) *FixedIPOperation {
	return &FixedIPOperation{Kind: "delete", Vendor: b.Vendor, ID: fixedPointer(b.ID), IP: b.IP, MAC: b.MAC, Comment: b.Comment, Server: b.Server, Interface: b.Interface, Gateway: b.Gateway, Expected: expected, Summary: summary}
}
