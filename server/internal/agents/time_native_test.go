package agents

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/taskstatus"
)

func TestLocalWallZoneTransitionsAgainstPython(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 for live Python zone oracle")
	}
	inputs := [][2]string{
		{"America/New_York", "2026-03-08T02:30:00"}, {"America/New_York", "2026-11-01T01:30:00"},
		{"Europe/Berlin", "2026-03-29T02:30:00"}, {"Europe/Berlin", "2026-10-25T02:30:00"},
		{"Australia/Lord_Howe", "2026-10-04T02:15:00"}, {"Australia/Lord_Howe", "2026-04-05T01:45:00"},
		{"Pacific/Apia", "2011-12-30T12:00:00"},
	}
	data, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	const oracle = `import datetime,json,sys,zoneinfo
rows=json.load(sys.stdin)
print(json.dumps([[zone,text,datetime.datetime.fromisoformat(text).replace(tzinfo=zoneinfo.ZoneInfo(zone),fold=0).timestamp()] for zone,text in rows]))`
	command := exec.CommandContext(t.Context(), filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "-c", oracle)
	command.Stdin = bytes.NewReader(data)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python zone oracle: %v %s", err, output)
	}
	var rows [][]json.RawMessage
	if err := json.Unmarshal(output, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(inputs) {
		t.Fatal("zone oracle input binding count changed")
	}
	for index, row := range rows {
		if len(row) != 3 {
			t.Fatal("zone oracle shape changed")
		}
		var zone, text string
		var epoch float64
		if json.Unmarshal(row[0], &zone) != nil || json.Unmarshal(row[1], &text) != nil || json.Unmarshal(row[2], &epoch) != nil || zone != inputs[index][0] || text != inputs[index][1] {
			t.Fatal("zone oracle row lost original input binding")
		}
		location, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		wall, _, valid := taskstatus.ParseISO(text)
		if !valid {
			t.Fatal("invalid fixture", text)
		}
		actual := resolveLocalWall(wall, location)
		if float64(actual.Unix()) != epoch {
			t.Fatalf("zone=%s wall=%s native=%s epoch=%d Python=%.0f", zone, text, actual, actual.Unix(), epoch)
		}
	}
}
