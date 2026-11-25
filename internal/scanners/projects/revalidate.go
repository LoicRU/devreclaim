package projects

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LoicRU/devreclaim/internal/core"
)

// Revalidate checks again, right before a deletion, that the candidate is
// still a rebuildable artifact: its marker files must still be next to it.
// The folder could have changed since the scan.
func Revalidate(c core.Candidate) error {
	dir, name := filepath.Dir(c.Path), filepath.Base(c.Path)

	if c.Kind != "" && c.Kind != name {
		return fmt.Errorf("the folder is now named %q, not %q", name, c.Kind)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read the project folder: %w", err)
	}

	siblings := make(map[string]bool, len(entries))
	for _, e := range entries {
		siblings[e.Name()] = true
	}

	f, ok := classify(dir, name, siblings)
	if !ok {
		return fmt.Errorf("%s is no longer next to a project marker file", name)
	}

	if !f.Regenerable {
		return fmt.Errorf("%s can no longer be rebuilt safely (%s)", name, f.NotCounted)
	}

	return nil
}
