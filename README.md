# Reclaim

See exactly what's eating your dev disk on Windows — projects, caches, WSL, Docker — and reclaim it safely.

> **Status**: early development (M0). Not ready for use yet. See [`docs/PLAN.md`](docs/PLAN.md)
> for the full roadmap, and [`docs/what-gets-deleted.md`](docs/what-gets-deleted.md) (coming soon)
> for exactly what each category proposes to remove.

## Install

Not released yet. Once `v0.1.0` ships: GitHub Releases, then winget/scoop.

## Quick start

```
devreclaim scan
devreclaim scan --category package-caches
devreclaim wsl status
devreclaim clean                 # dry run: shows the plan
devreclaim clean --apply         # asks, then sends folders to the Recycle Bin
devreclaim wsl compact --distro Ubuntu           # dry run
devreclaim wsl compact --distro Ubuntu --apply   # admin terminal required
```

`scan` and `wsl status` never delete anything. `clean` does nothing without `--apply`.

## Safety

- Nothing is deleted without `--apply`.
- Deletion defaults to the Recycle Bin (`--trash`) until the tool has an established track record.
- Every deletion is logged. No telemetry, no network calls for scanning or cleaning.

See [`docs/safety.md`](docs/safety.md) for the safeguards and [`docs/PLAN.md`](docs/PLAN.md) for the full plan.

## Contributing

Commit messages follow [`docs/COMMITS.md`](docs/COMMITS.md). Enable the hook with
`git config core.hooksPath .githooks`. Lint with `golangci-lint run`.

## License

MIT — see [LICENSE](LICENSE).
