package wsl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LoicRU/reclaim/internal/core"
	"github.com/LoicRU/reclaim/internal/platform/exec"
)

const reclaimNote = "estimate (upper bound): the real gain depends on fstrim and compaction"

// Scanner reports the size of each WSL2 virtual disk and how much of it is
// not used inside the distro. It never modifies anything.
//
// Reading the usage runs `df` inside each distro, which starts the distros
// that are stopped.
type Scanner struct {
	Runner exec.CommandRunner
	// FileSize returns the size in bytes of the file at path.
	FileSize func(path string) (int64, error)
}

// New returns a Scanner that reads real file sizes.
func New(runner exec.CommandRunner) *Scanner {
	return &Scanner{Runner: runner, FileSize: statSize}
}

func statSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

// Name implements core.Scanner.
func (*Scanner) Name() string { return "wsl" }

// Category implements core.Scanner.
func (*Scanner) Category() core.Category { return core.CategoryWSL }

// Scan implements core.Scanner.
func (s *Scanner) Scan(ctx context.Context, _ core.ScanOptions) (<-chan core.Candidate, error) {
	distros, err := s.listDistros(ctx)
	if err != nil {
		return nil, err
	}

	ch := make(chan core.Candidate)

	go func() {
		defer close(ch)

		for _, d := range distros {
			select {
			case ch <- s.candidate(ctx, d):
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

func (s *Scanner) listDistros(ctx context.Context) ([]Distro, error) {
	stdout, stderr, err := s.Runner.Run(ctx, "reg.exe", "query", lxssKey, "/s")
	if err != nil {
		return nil, fmt.Errorf("read WSL registry key (is WSL installed?): %w: %s",
			err, strings.TrimSpace(exec.DecodeOutput(stderr)))
	}

	return ParseRegQuery(exec.DecodeOutput(stdout)), nil
}

func (s *Scanner) candidate(ctx context.Context, d Distro) core.Candidate {
	c := core.Candidate{
		ID:       core.NewID(core.CategoryWSL, d.Name),
		Category: core.CategoryWSL,
		Kind:     "wsl-vhdx",
		Path:     d.VHDXPath(),
		Label:    d.Name,
		Risk:     core.RiskLow,
		Action:   core.ActionSpec{Type: "wsl-compact", NeedsAdmin: true},
		Reasons: []string{
			"the virtual disk grows with the distro but never shrinks by itself",
		},
	}

	if strings.HasPrefix(d.Name, "docker-desktop") {
		c.Warnings = append(c.Warnings, "managed by Docker Desktop: compact it from Docker Desktop instead")
	}

	if d.Version != 2 {
		c.Kind = "wsl1-distro"
		c.Path = d.BasePath
		c.Action = core.ActionSpec{}
		c.Warnings = append(c.Warnings, "WSL1 distro: no virtual disk to compact")

		return c
	}

	size, err := s.FileSize(c.Path)
	if err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("cannot read the virtual disk size: %v", err))

		return c
	}

	c.SizeBytes = size

	used, err := s.usedBytes(ctx, d.Name)
	if err != nil {
		c.Warnings = append(c.Warnings, fmt.Sprintf("cannot read the space used inside the distro: %v", err))

		return c
	}

	c.ReclaimBytes = max(size-used, 0)
	c.ReclaimNote = reclaimNote

	return c
}

func (s *Scanner) usedBytes(ctx context.Context, distro string) (int64, error) {
	stdout, stderr, err := s.Runner.Run(ctx, "wsl.exe", "-d", distro, "-u", "root", "--", "df", "-P", "-B1", "/")
	if err != nil {
		return 0, fmt.Errorf("%w: %s", err, strings.TrimSpace(exec.DecodeOutput(stderr)))
	}

	return ParseDfUsed(exec.DecodeOutput(stdout))
}
