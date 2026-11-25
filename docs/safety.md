# Safety

`devreclaim` never deletes anything unless you pass `--apply`.

## What clean does

1. Scans (read-only), then builds a plan: only rebuildable items with a gain above zero.
2. Prints the plan. Without `--apply` it stops there (dry run).
3. With `--apply`, asks for confirmation (skip with `--yes`).
4. For each folder, right before removing it, checks again that:
   - the path is a real folder, not a link or junction, and is reached without any link;
   - it is strictly inside a scanned root (project artifacts);
   - it is not a protected folder, nor contains one (drive root, Windows, Program Files,
     your user folder, Documents, Desktop, Pictures, Downloads, Videos, Music);
   - it has no `.git`, `.ssh` or `.gnupg` component;
   - the project marker files (lockfile, `package.json`...) are still there.
5. Sends the folder to the Recycle Bin (default) or deletes it for good with `--delete`.
6. Appends one line per item to `%LOCALAPPDATA%\devreclaim\logs\YYYY-MM-DD.jsonl`.
   If the journal cannot be written, cleaning stops.

Package caches use the tool's own command when it answered (`npm cache clean --force`,
`go clean -cache`...). Only a short allow-list of programs can be run.

## Medium-risk items

Gradle and Maven caches are skipped unless you add `--include-medium-risk`.

## How to undo

Folders sent to the Recycle Bin can be restored from it. Cache commands and `--delete`
cannot be undone; caches are downloaded again by their tool.
