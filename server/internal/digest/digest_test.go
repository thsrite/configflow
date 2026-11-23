package digest

import "testing"

func TestSHA256Hex(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "empty",
			in:   "",
			want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name: "abc",
			in:   "abc",
			want: "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			name: "utf8",
			in:   "配置流",
			want: "fbff01e4d65762bc756bd542b5ce931bf40843d2ef2338056f77398f47097c27",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := SHA256Hex([]byte(tc.in)); got != tc.want {
				t.Fatalf("SHA256Hex(%q) = %q, want %q", tc.in, got, tc.want)
			}
			if got := SHA256HexString(tc.in); got != tc.want {
				t.Fatalf("SHA256HexString(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
