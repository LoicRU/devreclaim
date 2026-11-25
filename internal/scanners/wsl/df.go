package wsl

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ParseDfUsed returns the "Used" column, in bytes, of `df -P -B1 /`.
func ParseDfUsed(out string) (int64, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 2 {
		return 0, errors.New("df output has no data line")
	}

	// Columns: Filesystem 1-blocks Used Available Capacity Mounted-on
	fields := strings.Fields(lines[1])
	if len(fields) < 6 {
		return 0, fmt.Errorf("unexpected df line %q", lines[1])
	}

	used, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse used bytes %q: %w", fields[2], err)
	}

	return used, nil
}
