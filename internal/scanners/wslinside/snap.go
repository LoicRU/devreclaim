package wslinside

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
)

var (
	numericRev = regexp.MustCompile(`^[0-9]+$`)
	snapName   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// SnapRev is one installed revision of a snap.
type SnapRev struct {
	Name     string
	Rev      string
	Disabled bool
}

// ParseSnapList reads the output of `snap list --all`. Revisions that are not
// plain numbers (local installs) are ignored.
func ParseSnapList(out string) []SnapRev {
	var revs []SnapRev

	for i, line := range strings.Split(strings.ReplaceAll(out, "\r", ""), "\n") {
		f := strings.Fields(line)
		if i == 0 && len(f) > 0 && f[0] == "Name" || len(f) < 3 {
			continue
		}

		r := SnapRev{Name: f[0], Rev: f[2]}

		if len(f) >= 6 {
			for _, note := range strings.Split(f[len(f)-1], ",") {
				if note == "disabled" {
					r.Disabled = true
				}
			}
		}

		if numericRev.MatchString(r.Rev) && snapName.MatchString(r.Name) {
			revs = append(revs, r)
		}
	}

	return revs
}

// removable returns the disabled revisions that can go: only when the same
// snap still has an active revision. A snap with no active revision is never
// touched, because removing its last revision uninstalls it.
func removable(revs []SnapRev) []SnapRev {
	active := map[string]bool{}

	for _, r := range revs {
		if !r.Disabled {
			active[r.Name] = true
		}
	}

	var out []SnapRev

	for _, r := range revs {
		if r.Disabled && active[r.Name] {
			out = append(out, r)
		}
	}

	return out
}

func snapCandidates(ctx context.Context, runner exec.CommandRunner, distro string, minSize int64) []core.Candidate {
	user := inDistro{inner: runner, distro: distro}

	stdout, _, err := user.Run(ctx, "snap", "list", "--all")
	if err != nil {
		return nil
	}

	root := inDistro{inner: runner, distro: distro, user: "root"}

	var out []core.Candidate

	for _, r := range removable(ParseSnapList(exec.DecodeOutput(stdout))) {
		file := fmt.Sprintf("/var/lib/snapd/snaps/%s_%s.snap", r.Name, r.Rev)

		sizeOut, _, statErr := root.Run(ctx, "stat", "-c", "%s", file)
		if statErr != nil {
			continue
		}

		size, parseErr := strconv.ParseInt(strings.TrimSpace(exec.DecodeOutput(sizeOut)), 10, 64)
		if parseErr != nil || size <= 0 || size < minSize {
			continue
		}

		label := fmt.Sprintf("%s: snap %s r%s", distro, r.Name, r.Rev)

		out = append(out, core.Candidate{
			ID: core.NewID(core.CategoryPackageCaches, label), Category: core.CategoryPackageCaches,
			Kind: "snap-revision", Path: file, Label: label,
			SizeBytes: size, ReclaimBytes: size, Regenerable: true,
			RegenHint: "old revision kept by snap: the active one stays installed",
			Risk:      core.RiskLow,
			Reasons:   []string{"disabled revision of a snap that has an active revision"},
			Action: core.ActionSpec{
				Type: "run-command", NeedsAdmin: false,
				Command: []string{"wsl.exe", "-d", distro, "-u", "root", "--", "snap", "remove", r.Name, "--revision=" + r.Rev},
			},
		})
	}

	return out
}
