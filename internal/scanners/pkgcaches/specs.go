// Package pkgcaches finds the global caches of package managers and build
// tools (npm, pip, cargo, Go...). It only measures them: nothing is deleted.
package pkgcaches

import (
	"path/filepath"
	"strings"

	"github.com/LoicRU/devreclaim/internal/core"
)

// spec describes one cache and how to locate and clean it.
type spec struct {
	name string
	// locate is the command that prints the cache folder (nil when the tool has none).
	locate []string
	// parse extracts the folder from the output of locate (nil: last non-empty line).
	parse func(out string) string
	// fallback returns the usual Windows folders, used when the tool does not answer.
	fallback func(env Env) string
	// clean is the native command that empties the cache (nil: delete the folder).
	clean []string
	risk  core.Risk
	// counted is false when the folder size is not a safe estimate of the gain.
	counted bool
	note    string
	warning string
	hint    string
}

// Env gives access to environment variables so tests can control them.
type Env func(key string) string

func home(env Env) string { return env("USERPROFILE") }

func under(base string, parts ...string) string {
	if base == "" {
		return ""
	}

	return filepath.Join(append([]string{base}, parts...)...)
}

func cargoHome(env Env) string {
	if v := env("CARGO_HOME"); v != "" {
		return v
	}

	return under(home(env), ".cargo")
}

func gradleHome(env Env) string {
	if v := env("GRADLE_USER_HOME"); v != "" {
		return v
	}

	return under(home(env), ".gradle")
}

func nugetParse(out string) string {
	const key = "global-packages:"

	for _, line := range strings.Split(out, "\n") {
		if _, after, ok := strings.Cut(line, key); ok {
			return strings.TrimSpace(after)
		}
	}

	return ""
}

func specs() []spec {
	return []spec{
		{
			name: "npm", locate: []string{"npm", "config", "get", "cache"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "npm-cache") },
			clean:    []string{"npm", "cache", "clean", "--force"},
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next npm install",
		},
		{
			name: "pnpm store", locate: []string{"pnpm", "store", "path"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "pnpm", "store") },
			clean:    []string{"pnpm", "store", "prune"},
			risk:     core.RiskLow, counted: false,
			note: "shared with your projects: pnpm store prune only removes unreferenced packages",
			hint: "pnpm store prune",
		},
		{
			name: "yarn", locate: []string{"yarn", "cache", "dir"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "Yarn", "Cache") },
			clean:    []string{"yarn", "cache", "clean"},
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next yarn install",
		},
		{
			name: "pip", locate: []string{"pip", "cache", "dir"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "pip", "Cache") },
			clean:    []string{"pip", "cache", "purge"},
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next pip install",
		},
		{
			name: "uv", locate: []string{"uv", "cache", "dir"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "uv", "cache") },
			clean:    []string{"uv", "cache", "clean"},
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next uv sync",
		},
		{
			name:     "cargo registry",
			fallback: func(e Env) string { return under(cargoHome(e), "registry") },
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next cargo build",
		},
		{
			name:     "cargo git",
			fallback: func(e Env) string { return under(cargoHome(e), "git") },
			risk:     core.RiskSafe, counted: true, hint: "cloned again on the next cargo build",
		},
		{
			name:   "NuGet packages",
			locate: []string{"dotnet", "nuget", "locals", "global-packages", "--list"},
			parse:  nugetParse,
			fallback: func(e Env) string {
				return under(home(e), ".nuget", "packages")
			},
			clean: []string{"dotnet", "nuget", "locals", "all", "--clear"},
			risk:  core.RiskSafe, counted: true, hint: "restored on the next dotnet restore",
		},
		{
			name:     "Gradle caches",
			fallback: func(e Env) string { return under(gradleHome(e), "caches") },
			risk:     core.RiskMedium, counted: true,
			warning: "the next Gradle build re-downloads everything and can be slow or need network",
			hint:    "downloaded again on the next Gradle build",
		},
		{
			name:     "Maven repository",
			fallback: func(e Env) string { return under(home(e), ".m2", "repository") },
			risk:     core.RiskMedium, counted: true,
			warning: "may hold artifacts that are not on a remote repository (locally installed builds)",
			hint:    "downloaded again on the next Maven build",
		},
		{
			name: "Go build cache", locate: []string{"go", "env", "GOCACHE"},
			fallback: func(e Env) string { return under(e("LOCALAPPDATA"), "go-build") },
			clean:    []string{"go", "clean", "-cache"},
			risk:     core.RiskSafe, counted: true, hint: "rebuilt on the next go build",
		},
		{
			name: "Go module cache", locate: []string{"go", "env", "GOMODCACHE"},
			fallback: func(e Env) string { return under(home(e), "go", "pkg", "mod") },
			clean:    []string{"go", "clean", "-modcache"},
			risk:     core.RiskSafe, counted: true, hint: "downloaded again on the next go build",
		},
	}
}
