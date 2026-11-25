# Commit rules

Commits are small and frequent: one logical change each (one function, one test,
one flag, one doc fix). Every commit must build and keep tests green.

## Format

```
type(scope): summary
```

- **Language**: English.
- **Types**: `feat`, `fix`, `test`, `docs`, `refactor`, `chore`, `ci`, `build`, `perf`.
- **Scope** (optional): the package or area, lowercase — `cli`, `core`, `wsl`,
  `safety`, `report`, `docs`, `ci`…
- **Summary**: imperative mood ("add", not "added"), 72 characters max, no
  trailing period.
- **Body** (optional): separated by a blank line; explain *why*, not *what*.
- No attribution lines for AI tools in commit messages.

## Examples

```
feat(wsl): read distro names from the registry
test(wsl): cover UTF-16 output of wsl.exe
fix(cli): return exit code 2 on unknown flag
docs(plan): mark M1 as done
chore: bump cobra to v1.10.2
```

## Enforcement

A `commit-msg` hook in `.githooks/` checks the format. Enable it once per clone:

```
git config core.hooksPath .githooks
```

## Before committing

```
gofumpt -l .
golangci-lint run
go test ./...
```
