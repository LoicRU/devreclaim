package cli

import (
	"os"
	"path/filepath"
	"time"

	"github.com/LoicRU/devreclaim/internal/clean"
	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/platform/trash"
	"github.com/LoicRU/devreclaim/internal/safety"
	"github.com/LoicRU/devreclaim/internal/scanners/pkgcaches"
	"github.com/LoicRU/devreclaim/internal/scanners/projects"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
)

// allScanners returns the scanners used by scan and clean.
func allScanners() []core.Scanner {
	return []core.Scanner{projects.New(), pkgcaches.New(exec.OSRunner{})}
}

func wslScanner() core.Scanner { return wsl.New(exec.OSRunner{}) }

// realExecutor wires the executor to the real machine.
func realExecutor(roots []string, mode clean.Mode, journal *clean.Journal) *clean.Executor {
	return &clean.Executor{
		Guard:   safety.DefaultGuard(os.Getenv),
		Roots:   roots,
		Mode:    mode,
		Trash:   trash.Move,
		Remove:  os.RemoveAll,
		Runner:  exec.OSRunner{},
		Verify:  verifyArtifact,
		Journal: journal,
		Now:     time.Now,
	}
}

// verifyArtifact re-checks project artifacts right before they are removed.
func verifyArtifact(c core.Candidate) error {
	if c.Category == core.CategoryProjectArtifacts {
		return projects.Revalidate(c)
	}

	return nil
}

// defaultLogDir is %LOCALAPPDATA%\devreclaim\logs, or the user cache folder.
func defaultLogDir() string {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "devreclaim", "logs")
	}

	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "devreclaim", "logs")
	}

	return filepath.Join(os.TempDir(), "devreclaim", "logs")
}
