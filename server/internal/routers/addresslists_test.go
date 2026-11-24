package routers_test

import (
	"testing"

	"configflow/internal/routers/ros"
)

func TestAddressDigestMutationIsDetected(t *testing.T) {
	rows := []ros.Row{{".id": "*1", "list": "L", "comment": "中文;x=1"}}
	digest := ros.DigestAddresses(rows)
	rows[0]["comment"] = "changed"
	if ros.DigestAddresses(rows) == digest {
		t.Fatal("content mutation escaped proof")
	}
}
