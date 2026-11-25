package report

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var sizeUnits = map[string]float64{
	"": 1, "B": 1,
	"K": 1 << 10, "KB": 1 << 10,
	"M": 1 << 20, "MB": 1 << 20,
	"G": 1 << 30, "GB": 1 << 30,
	"T": 1 << 40, "TB": 1 << 40,
}

// ParseSize reads sizes such as "512", "100MB", "1.5 GB" or "10k" into bytes.
// Units are 1024-based, like HumanSize.
func ParseSize(s string) (int64, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	if t == "" {
		return 0, errors.New("empty size")
	}

	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		i++
	}

	number, unit := t[:i], strings.TrimSpace(t[i:])
	if number == "" {
		return 0, fmt.Errorf("invalid size %q", s)
	}

	value, err := strconv.ParseFloat(number, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q: %w", s, err)
	}

	factor, ok := sizeUnits[unit]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q in size %q", unit, s)
	}

	return int64(value * factor), nil
}
