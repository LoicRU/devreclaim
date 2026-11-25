package exec

import (
	"bytes"
	"unicode/utf16"
)

// DecodeUTF16LE converts UTF-16 little-endian bytes (as printed by wsl.exe for
// its own messages) to a string. A leading byte order mark is dropped.
func DecodeUTF16LE(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xFF, 0xFE})

	units := make([]uint16, len(b)/2)
	for i := range units {
		units[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}

	return string(utf16.Decode(units))
}

// DecodeOutput returns b as text. wsl.exe prints its own messages in UTF-16LE
// but passes through the output of commands run inside a distro as UTF-8, so
// the encoding is detected from the NUL bytes UTF-16 leaves between ASCII
// characters.
func DecodeOutput(b []byte) string {
	if looksUTF16LE(b) {
		return DecodeUTF16LE(b)
	}

	return string(b)
}

func looksUTF16LE(b []byte) bool {
	if bytes.HasPrefix(b, []byte{0xFF, 0xFE}) {
		return true
	}

	if len(b) < 4 {
		return false
	}

	nuls := 0

	for i := 1; i < len(b); i += 2 {
		if b[i] == 0 {
			nuls++
		}
	}

	return nuls*2 >= len(b)/2
}
