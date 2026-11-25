package exec

import "testing"

// utf16le encodes ASCII text the way wsl.exe prints it.
func utf16le(s string, bom bool) []byte {
	var out []byte
	if bom {
		out = append(out, 0xFF, 0xFE)
	}

	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}

	return out
}

func TestDecodeUTF16LE(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"plain", utf16le("Ubuntu", false), "Ubuntu"},
		{"with BOM", utf16le("Ubuntu", true), "Ubuntu"},
		{"accent", utf16le("é", false), "é"},
		{"empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeUTF16LE(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDecodeOutputDetectsEncoding(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want string
	}{
		{"utf16", utf16le("There is no distribution with the supplied name.", false), "There is no distribution with the supplied name."},
		{"utf16 with BOM", utf16le("Error", true), "Error"},
		{"utf8", []byte("Filesystem 1B-blocks Used"), "Filesystem 1B-blocks Used"},
		{"short utf8", []byte("ok"), "ok"},
		{"empty", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DecodeOutput(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
