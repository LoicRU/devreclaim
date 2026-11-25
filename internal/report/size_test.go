package report

import "testing"

func TestParseSize(t *testing.T) {
	tests := map[string]int64{
		"0":      0,
		"512":    512,
		"512B":   512,
		"10k":    10 << 10,
		"10KB":   10 << 10,
		"100MB":  100 << 20,
		"100 mb": 100 << 20,
		"1.5GB":  3 << 29,
		"2T":     2 << 40,
		" 50MB ": 50 << 20,
	}

	for in, want := range tests {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Fatalf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
}

func TestParseSizeErrors(t *testing.T) {
	for _, in := range []string{"", "  ", "MB", "abc", "12XB", "1.2.3MB", "-5MB"} {
		if _, err := ParseSize(in); err == nil {
			t.Fatalf("expected an error for %q", in)
		}
	}
}
