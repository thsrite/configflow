package lifecycle

import "testing"

func TestSignalClear(t *testing.T) {
	for _, tt := range []struct {
		name   string
		notify int
	}{
		{"empty", 0},
		{"pending", 1},
		{"coalesced", 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var signal Signal
			for range tt.notify {
				signal.Notify()
			}
			signal.Clear()
			select {
			case <-signal.channel():
				t.Fatal("Clear left a pending wake")
			default:
			}
			signal.Notify()
			select {
			case <-signal.channel():
			default:
				t.Fatal("Notify after Clear did not wake")
			}
		})
	}
}
