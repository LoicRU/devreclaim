// Package projects finds build artifacts inside developer projects
// (node_modules, target, .venv...) that can be rebuilt from the sources.
package projects

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"github.com/LoicRU/devreclaim/internal/core"
)

// found is an artifact folder discovered next to a project marker file.
type found struct {
	Path        string
	ProjectDir  string
	Root        string // scan root the artifact was found under
	Kind        string
	Regenerable bool
	RegenHint   string
	Risk        core.Risk
	// NotCounted explains why the artifact is not counted as reclaimable when
	// it cannot be rebuilt safely.
	NotCounted string
}

var lockfiles = []string{
	"package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", "bun.lock", "bun.lockb",
}

var (
	pythonRequirements = []string{"requirements.txt", "pyproject.toml", "Pipfile", "poetry.lock", "uv.lock", "setup.py"}
	dotnetProjects     = []string{".csproj", ".fsproj", ".vbproj", ".sln"}
	gradleMarkers      = []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts"}
	buildMarkers       = []string{
		"package.json", "pyproject.toml", "setup.py", "pom.xml",
		"build.gradle", "build.gradle.kts", "CMakeLists.txt",
	}
)

// artifactNames are the folder names the scanner knows. They are never
// walked into and are ignored when measuring a project's activity.
var artifactNames = map[string]bool{
	"node_modules": true, "target": true, ".venv": true, "venv": true, "__pycache__": true,
	"bin": true, "obj": true, ".gradle": true, "build": true, "dist": true,
}

// classify decides whether the folder name inside dir is a rebuildable
// artifact. siblings holds the names of everything in dir.
func classify(dir, name string, siblings map[string]bool) (found, bool) {
	f := found{Kind: name, Regenerable: true, Risk: core.RiskLow}

	switch name {
	case "node_modules":
		if !siblings["package.json"] {
			return found{}, false
		}

		f.RegenHint = "npm ci / pnpm install / yarn install"

		if !hasAny(siblings, lockfiles...) {
			f.Regenerable, f.Risk = false, core.RiskMedium
			f.NotCounted = "no lockfile: not counted"
		}
	case "target":
		switch {
		case siblings["Cargo.toml"]:
			f.RegenHint = "cargo build"
		case siblings["pom.xml"]:
			f.RegenHint = "mvn package"
		default:
			return found{}, false
		}
	case ".venv", "venv":
		if _, err := os.Stat(filepath.Join(dir, name, "pyvenv.cfg")); err != nil {
			return found{}, false
		}

		f.RegenHint = "recreate it from the requirements file"

		if !hasAny(siblings, pythonRequirements...) {
			f.Regenerable, f.Risk = false, core.RiskMedium
			f.NotCounted = "no requirements file: not counted"
		}
	case "bin", "obj":
		if !hasSuffix(siblings, dotnetProjects...) {
			return found{}, false
		}

		f.RegenHint = "dotnet build"
	case ".gradle":
		if !hasAny(siblings, gradleMarkers...) {
			return found{}, false
		}

		f.RegenHint = "gradle build"
	case "build", "dist":
		if !hasAny(siblings, buildMarkers...) {
			return found{}, false
		}

		f.RegenHint = "rebuild the project"

		if !gitignored(dir, name) {
			f.Regenerable, f.Risk = false, core.RiskMedium
			f.NotCounted = "not in .gitignore: not counted"
		}
	case "__pycache__":
		f.RegenHint = "regenerated automatically by Python"
		f.Risk = core.RiskSafe
	default:
		return found{}, false
	}

	return f, true
}

func hasAny(names map[string]bool, wanted ...string) bool {
	for _, w := range wanted {
		if names[w] {
			return true
		}
	}

	return false
}

func hasSuffix(names map[string]bool, suffixes ...string) bool {
	for name := range names {
		for _, s := range suffixes {
			if strings.HasSuffix(name, s) {
				return true
			}
		}
	}

	return false
}

// gitignored reports whether dir/.gitignore lists name with one of the usual
// spellings. It is a heuristic, not a full gitignore engine.
func gitignored(dir, name string) bool {
	//nolint:gosec // the path is built from a folder the user asked to scan
	f, err := os.Open(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return false
	}
	defer f.Close()

	spellings := map[string]bool{
		name: true, name + "/": true, "/" + name: true, "/" + name + "/": true,
		"**/" + name: true, "**/" + name + "/": true,
	}

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if spellings[strings.TrimSpace(sc.Text())] {
			return true
		}
	}

	return false
}

// skipDir tells whether a folder that is not an artifact is worth walking.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "AppData" ||
		name == "$RECYCLE.BIN" || name == "System Volume Information"
}
