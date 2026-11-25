// Package wslinside looks for reclaimable space inside WSL distros: package
// caches and old snap revisions. It runs commands through wsl.exe, which
// starts a distro that is stopped. Nothing is modified.
package wslinside

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/scanners/pkgcaches"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
)

// Scanner scans every WSL distro except the ones Docker Desktop manages.
type Scanner struct {
	Runner  exec.CommandRunner
	Distros func(ctx context.Context) ([]wsl.Distro, error)
}

// New returns a Scanner that uses the real machine.
func New(runner exec.CommandRunner) *Scanner {
	return &Scanner{
		Runner: runner,
		Distros: func(ctx context.Context) ([]wsl.Distro, error) {
			return wsl.ListDistros(ctx, runner)
		},
	}
}

// Name implements core.Scanner.
func (*Scanner) Name() string { return "wsl-inside" }

// Category implements core.Scanner.
func (*Scanner) Category() core.Category { return core.CategoryPackageCaches }

// Scan implements core.Scanner.
func (s *Scanner) Scan(ctx context.Context, opts core.ScanOptions) (<-chan core.Candidate, error) {
	distros, err := s.Distros(ctx)
	if err != nil {
		return nil, err
	}

	ch := make(chan core.Candidate)

	var wg sync.WaitGroup

	for _, d := range distros {
		if strings.HasPrefix(d.Name, "docker-desktop") {
			continue
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			for _, c := range s.scanDistro(ctx, d, opts) {
				select {
				case ch <- c:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}

// inDistro runs commands inside one distro with wsl.exe.
type inDistro struct {
	inner  exec.CommandRunner
	distro string
	user   string // "" = the distro's default user
}

func (r inDistro) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	a := []string{"-d", r.distro}
	if r.user != "" {
		a = append(a, "-u", r.user)
	}

	a = append(a, "--", name)

	return r.inner.Run(ctx, "wsl.exe", append(a, args...)...)
}

func (s *Scanner) scanDistro(ctx context.Context, d wsl.Distro, opts core.ScanOptions) []core.Candidate {
	run := inDistro{inner: s.Runner, distro: d.Name}

	env, ok := distroEnv(ctx, run)
	if !ok {
		return nil
	}

	caches := &pkgcaches.Scanner{
		Runner: run,
		Env:    env,
		IsDir: func(p string) bool {
			_, _, err := run.Run(ctx, "test", "-d", p)

			return err == nil
		},
		DirSize: func(c context.Context, dir string) int64 { return duBytes(c, run, dir) },
		Linux:   true, Prefix: d.Name + ": ", CommandOnly: true,
		WrapCommand: func(cmd []string) []string {
			return append([]string{"wsl.exe", "-d", d.Name, "--"}, cmd...)
		},
	}

	ch, err := caches.Scan(ctx, opts)
	if err != nil {
		return nil
	}

	out := core.Collect(ch)

	return append(out, snapCandidates(ctx, s.Runner, d.Name, opts.MinSize)...)
}

// distroEnv reads the few variables the cache locations depend on.
func distroEnv(ctx context.Context, run exec.CommandRunner) (pkgcaches.Env, bool) {
	stdout, _, err := run.Run(ctx, "sh", "-c", `echo "$HOME"; echo "$CARGO_HOME"; echo "$GRADLE_USER_HOME"`)
	if err != nil {
		return nil, false
	}

	lines := strings.Split(strings.ReplaceAll(exec.DecodeOutput(stdout), "\r", ""), "\n")
	vals := map[string]string{}

	for i, key := range []string{"HOME", "CARGO_HOME", "GRADLE_USER_HOME"} {
		if i < len(lines) {
			vals[key] = strings.TrimSpace(lines[i])
		}
	}

	if vals["HOME"] == "" {
		return nil, false
	}

	return func(k string) string { return vals[k] }, true
}

// duBytes returns the size of dir measured by `du -sb` inside the distro.
func duBytes(ctx context.Context, run exec.CommandRunner, dir string) int64 {
	stdout, _, err := run.Run(ctx, "du", "-sb", dir)
	if err != nil && len(stdout) == 0 {
		return 0
	}

	fields := strings.Fields(exec.DecodeOutput(stdout))
	if len(fields) == 0 {
		return 0
	}

	n, parseErr := strconv.ParseInt(fields[0], 10, 64)
	if parseErr != nil {
		return 0
	}

	return n
}
