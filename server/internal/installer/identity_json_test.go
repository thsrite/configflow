package installer

import (
	"testing"
)

func TestInstallationIdentityJSONGolden(t *testing.T) {
	tests := []struct {
		name, input, want string
		compact           bool
	}{
		{"spaced", `{"z":1,"a":"设备😀\u007f","list":[1.0,1e2,1e-7,1e16,0.0001]}`, `{"a": "\u8bbe\u5907\ud83d\ude00\u007f", "list": [1.0, 100.0, 1e-07, 1e+16, 0.0001], "z": 1}`, false},
		{"compact", `{"b":["a",true,null],"a":{"s":"中文"}}`, `{"a":{"s":"\u4e2d\u6587"},"b":["a",true,null]}`, true},
		{"large integer", `{"n":123456789012345678901234567890}`, `{"n":123456789012345678901234567890}`, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := identityJSON([]byte(test.input), test.compact)
			if err != nil || string(got) != test.want {
				t.Fatalf("got %s, %v; want %s", got, err, test.want)
			}
		})
	}
}
