package routers

import (
	"encoding/json"
	"maps"
	"testing"

	"configflow/internal/routers/routerfake"
)

// fakeRow is a driver row as the fake device stores it.
func fakeRow(row Row) routerfake.Row {
	out := make(routerfake.Row, len(row))
	for key, value := range row {
		out[key] = value
	}
	return out
}

// deviceRow is a fake device row as the driver decodes its reply.
func deviceRow(row routerfake.Row) Row {
	data, _ := json.Marshal(row)
	var out Row
	_ = json.Unmarshal(data, &out)
	return out
}

// decodedRow decodes one JSON object the way device replies are decoded.
func decodedRow(t testing.TB, raw string) Row {
	t.Helper()
	var row Row
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatal(err)
	}
	return row
}

// sameTextRow: got equals the recorded row once that row is decoded the way
// device replies are (values as text, null members absent).
func sameTextRow(got any, recorded json.RawMessage) bool {
	data, err := json.Marshal(got)
	if err != nil {
		return false
	}
	var left, right Row
	return json.Unmarshal(data, &left) == nil && json.Unmarshal(recorded, &right) == nil && maps.Equal(left, right)
}
