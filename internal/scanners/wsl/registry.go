// Package wsl scans WSL2 distributions: how big their virtual disk is and how
// much of it is really used inside the distro.
package wsl

import (
	"regexp"
	"strconv"
	"strings"
)

// lxssKey is where WSL registers its distributions (docs/PLAN.md §5.3).
const lxssKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Lxss`

// Distro is one registered WSL distribution.
type Distro struct {
	Name     string
	BasePath string // folder that holds ext4.vhdx, without the \\?\ prefix
	Version  int    // 1 or 2
}

// VHDXPath returns where the distro's virtual disk lives (WSL2 only).
func (d Distro) VHDXPath() string {
	return strings.TrimRight(d.BasePath, `\`) + `\ext4.vhdx`
}

// regValueLine matches "    Name    REG_SZ    value" lines of `reg query`.
var regValueLine = regexp.MustCompile(`^\s*(\S+)\s+(REG_[A-Z_]+)\s+(.*?)\s*$`)

// ParseRegQuery extracts distributions from the output of
// `reg query <Lxss> /s`. Keys without a DistributionName are ignored.
func ParseRegQuery(out string) []Distro {
	var (
		distros []Distro
		cur     *Distro
	)

	flush := func() {
		if cur != nil && cur.Name != "" {
			distros = append(distros, *cur)
		}

		cur = nil
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "HKEY_") {
			flush()

			cur = &Distro{}

			continue
		}

		m := regValueLine.FindStringSubmatch(line)
		if m == nil || cur == nil {
			continue
		}

		name, kind, value := m[1], m[2], m[3]

		switch name {
		case "DistributionName":
			cur.Name = value
		case "BasePath":
			cur.BasePath = strings.TrimPrefix(value, `\\?\`)
		case "Version":
			if kind == "REG_DWORD" {
				if v, err := strconv.ParseInt(value, 0, 64); err == nil {
					cur.Version = int(v)
				}
			}
		}
	}

	flush()

	return distros
}
