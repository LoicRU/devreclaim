package safety

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realTemp returns a temp dir whose path contains no link.
func realTemp(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()

	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}

	return p
}

func mustRefuse(t *testing.T, g Guard, path string, roots []string, want string) {
	t.Helper()

	err := g.Check(path, roots)
	if err == nil {
		t.Fatalf("expected %q to be refused", path)
	}

	if want != "" && !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention %q", err, want)
	}
}

func TestAcceptsStrictDescendantOfRoot(t *testing.T) {
	root := realTemp(t)
	art := mkdir(t, root, "proj", "node_modules")

	if err := (Guard{}).Check(art, []string{root}); err != nil {
		t.Fatalf("unexpected refusal: %v", err)
	}
}

func TestRefusesRootItselfAndOutsideRoot(t *testing.T) {
	root := realTemp(t)
	other := mkdir(t, realTemp(t), "x")

	mustRefuse(t, Guard{}, root, []string{root}, "not inside")
	mustRefuse(t, Guard{}, other, []string{root}, "not inside")
}

func TestRefusesDriveRootAndRelativePaths(t *testing.T) {
	mustRefuse(t, Guard{}, filepath.VolumeName(os.TempDir())+string(filepath.Separator), nil, "drive root")
	mustRefuse(t, Guard{}, "node_modules", nil, "absolute")
}

func TestRefusesProtectedFolderAndItsParents(t *testing.T) {
	home := realTemp(t)
	docs := mkdir(t, home, "Documents")
	g := Guard{Protected: []string{home, docs}}

	mustRefuse(t, g, docs, nil, "protected")
	mustRefuse(t, g, home, nil, "protected")

	parent := filepath.Dir(home)
	mustRefuse(t, g, parent, nil, "contains the protected")
}

func TestRefusesGitSshAndGnupgFolders(t *testing.T) {
	root := realTemp(t)

	for _, name := range []string{".git", ".ssh", ".gnupg"} {
		p := mkdir(t, root, "proj", name)
		mustRefuse(t, Guard{}, p, []string{root}, name)

		inner := mkdir(t, p, "objects")
		mustRefuse(t, Guard{}, inner, []string{root}, name)
	}
}

func TestRootlessPathInsideUserDataIsRefused(t *testing.T) {
	home := realTemp(t)
	docs := mkdir(t, home, "Documents")
	inside := mkdir(t, docs, "Projets")
	cache := mkdir(t, home, ".cache", "pip")

	g := Guard{UserData: []string{docs}}

	mustRefuse(t, g, inside, nil, "personal folder")

	if err := g.Check(cache, nil); err != nil {
		t.Fatalf("a cache outside personal folders must be accepted: %v", err)
	}

	// With an explicit root the same folder is fine.
	if err := g.Check(mkdir(t, inside, "node_modules"), []string{docs}); err != nil {
		t.Fatalf("a folder under a scan root must be accepted: %v", err)
	}
}

func TestRefusesMissingPathAndFiles(t *testing.T) {
	root := realTemp(t)
	file := filepath.Join(root, "file.txt")

	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	mustRefuse(t, Guard{}, filepath.Join(root, "missing"), []string{root}, "cannot inspect")
	mustRefuse(t, Guard{}, file, []string{root}, "not a folder")
}

func TestRefusesSymlinkPointingOutsideRoot(t *testing.T) {
	root := realTemp(t)
	outside := mkdir(t, realTemp(t), "precious")
	link := filepath.Join(root, "node_modules")

	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	mustRefuse(t, Guard{}, link, []string{root}, "link")

	if _, err := os.Stat(outside); err != nil {
		t.Fatal("the target must be untouched")
	}
}

func TestRefusesPathReachedThroughLinkedParent(t *testing.T) {
	root := realTemp(t)
	target := mkdir(t, realTemp(t), "proj", "node_modules")
	link := filepath.Join(root, "linked")

	if err := os.Symlink(filepath.Dir(target), link); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}

	mustRefuse(t, Guard{}, filepath.Join(link, "node_modules"), []string{root}, "link")
}

func TestDefaultGuardProtectsHomeAndPersonalFolders(t *testing.T) {
	env := map[string]string{
		"USERPROFILE": filepath.Join(string(filepath.Separator), "u", "me"),
		"SystemRoot":  filepath.Join(string(filepath.Separator), "win"),
	}

	g := DefaultGuard(func(k string) string { return env[k] })

	want := []string{
		filepath.Join(string(filepath.Separator), "u", "me"),
		filepath.Join(string(filepath.Separator), "u", "me", "Documents"),
		filepath.Join(string(filepath.Separator), "win"),
	}

	for _, w := range want {
		found := false

		for _, p := range g.Protected {
			found = found || p == w
		}

		if !found {
			t.Fatalf("%q is not protected: %v", w, g.Protected)
		}
	}

	if len(g.UserData) == 0 {
		t.Fatal("expected personal folders")
	}
}
