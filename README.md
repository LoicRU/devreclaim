# Reclaim

See exactly what's eating your dev disk on Windows — projects, caches, WSL, Docker — and reclaim it safely.

> **Status**: early development (M0). Not ready for use yet. See [`docs/PLAN.md`](docs/PLAN.md)
> for the full roadmap, and [`docs/what-gets-deleted.md`](docs/what-gets-deleted.md) (coming soon)
> for exactly what each category proposes to remove.

## Install

Not released yet. Once `v0.1.0` ships: GitHub Releases, then winget/scoop.

## Quick start

```
reclaim scan
reclaim clean --apply
```

(not implemented yet)

## Safety

- Nothing is deleted without `--apply`.
- Deletion defaults to the Recycle Bin (`--trash`) until the tool has an established track record.
- Every deletion is logged. No telemetry, no network calls for scanning or cleaning.

See [`docs/PLAN.md`](docs/PLAN.md) for the full safety model.

## Contributing

Commit messages follow [`docs/COMMITS.md`](docs/COMMITS.md). Enable the hook with
`git config core.hooksPath .githooks`. Lint with `golangci-lint run`.

## License

MIT — see [LICENSE](LICENSE).
