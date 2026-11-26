package ros

import (
	"reflect"
	"strings"
	"testing"
)

// FuzzR07DecodeAddressRow drives the console row decoder with arbitrary device
// output and confirms every decoded row round-trips through EncodeAddressRow.
func FuzzR07DecodeAddressRow(f *testing.F) {
	for _, seed := range []string{
		"5:.id=*1;address=198.51.100.1;comment=cf-mihomo-nat;disabled=false;list=cf-mihomo-nat-dst",
		"0:", "1:", "1:=", "2:a=b;c", "2:a=1;a=2", "-1:", "+1:a=b", "3:comment=中文=x;list=L;.id=*FFFF",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, text string) {
		row := decodeAddressRow(text)
		if row == nil {
			return
		}
		delete(row, "creation-time")
		delete(row, "timeout")
		again := decodeAddressRow(strings.TrimSuffix(EncodeAddressRow(row), "\n"))
		if !reflect.DeepEqual(again, row) {
			t.Fatalf("%q decoded to %#v, re-encoded to %#v", text, row, again)
		}
	})
}

func TestAddressDigestNeverProvesCompositeFields(t *testing.T) {
	// Array values arrive as their JSON text (Row decoding).
	rows := []Row{{".id": "*1", "list": "L", "tags": `["lan"]`}}
	first := DigestAddresses(rows)
	rows[0]["tags"] = `["guest"]`
	if DigestAddresses(rows) == first {
		t.Fatal("different composite values collapsed into the same proof")
	}
	if digestableAddresses(rows) {
		t.Fatal("array-valued REST fields must be downloaded")
	}
}

func TestInvalidateAddressReadsClearsProofs(t *testing.T) {
	d := NewClientWithHTTP("", nil)
	done := d.ReuseAddressReads()
	defer done()
	d.addressReads["L"] = addressProof{Rows: []Row{{".id": "*1"}}}
	d.InvalidateAddressReads()
	if len(d.addressReads) != 0 {
		t.Fatal("exported invalidation kept a proof")
	}
}
