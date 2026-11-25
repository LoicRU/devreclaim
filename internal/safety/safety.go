// Package safety decides whether a folder may be deleted. It is the last
// check before anything is removed and it errs on the side of refusing.
package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// forbiddenNames are folders that are never deleted, wherever they are.
var forbiddenNames = []string{".git", ".ssh", ".gnupg"}

// Guard holds the folders that must never be deleted.
type Guard struct {
	// Protected paths are never deleted, nor any folder that contains one.
	Protected []string
	// UserData are personal folders (Documents, Desktop...). A folder inside
	// them can only be deleted when it is under an explicit scan root.
	UserData []string
}

// DefaultGuard builds the guard for the current user from environment variables.
func DefaultGuard(env func(string) string) Guard {
	home := env("USERPROFILE")
	if home == "" {
		home = env("HOME")
	}

	var g Guard

	add := func(dst *[]string, parts ...string) {
		if parts[0] == "" {
			return
		}

		*dst = append(*dst, filepath.Join(parts...))
	}

	add(&g.Protected, home)

	for _, name := range []string{"Documents", "Desktop", "Pictures", "Downloads", "Videos", "Music"} {
		add(&g.UserData, home, name)
		add(&g.Protected, home, name)
	}

	for _, key := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
		add(&g.Protected, env(key))
	}

	if drive := env("SystemDrive"); drive != "" {
		add(&g.Protected, drive+string(filepath.Separator))
	}

	return g
}

// Check returns nil only when path can be deleted. When roots is not empty,
// path must be strictly inside one of them.
func (g Guard) Check(path string, roots []string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not an absolute path", path)
	}

	clean := filepath.Clean(path)

	if filepath.Dir(clean) == clean {
		return fmt.Errorf("%q is a drive root", clean)
	}

	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		for _, bad := range forbiddenNames {
			if same(part, bad) {
				return fmt.Errorf("%q is inside a %s folder", clean, bad)
			}
		}
	}

	for _, p := range g.Protected {
		if same(clean, p) {
			return fmt.Errorf("%q is a protected folder", clean)
		}

		if inside(clean, p) {
			return fmt.Errorf("%q contains the protected folder %q", clean, p)
		}
	}

	if err := g.checkRoots(clean, roots); err != nil {
		return err
	}

	return checkOnDisk(clean)
}

func (g Guard) checkRoots(clean string, roots []string) error {
	if len(roots) == 0 {
		for _, u := range g.UserData {
			if inside(u, clean) {
				return fmt.Errorf("%q is inside the personal folder %q", clean, u)
			}
		}

		return nil
	}

	for _, r := range roots {
		if inside(filepath.Clean(r), clean) {
			return nil
		}
	}

	return fmt.Errorf("%q is not inside a scanned root", clean)
}

// checkOnDisk makes sure the path is a real folder reached without any link.
func checkOnDisk(clean string) error {
	info, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("cannot inspect %q: %w", clean, err)
	}

	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("%q is a link or junction", clean)
	}

	if !info.IsDir() {
		return fmt.Errorf("%q is not a folder", clean)
	}

	resolved, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", clean, err)
	}

	if !same(resolved, clean) {
		return errors.New("the path goes through a link: " + clean + " -> " + resolved)
	}

	return nil
}

func fold(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}

	return s
}

func same(a, b string) bool { return fold(filepath.Clean(a)) == fold(filepath.Clean(b)) }

// inside reports whether child is strictly inside parent.
func inside(parent, child string) bool {
	rel, err := filepath.Rel(fold(filepath.Clean(parent)), fold(filepath.Clean(child)))
	if err != nil || rel == "." {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
