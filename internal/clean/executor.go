package clean

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/LoicRU/devreclaim/internal/core"
	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/safety"
)

// Mode tells how folders are removed.
type Mode string

// Removal modes. The Recycle Bin is the default: it can be undone.
const (
	ModeTrash  Mode = "trash"
	ModeDelete Mode = "delete"
)

// Status of one reclaimed item.
const (
	StatusDone   = "done"
	StatusFailed = "failed"
)

// allowedCommands are the only programs clean will run.
var allowedCommands = map[string]bool{
	"npm": true, "pnpm": true, "yarn": true, "pip": true, "uv": true, "dotnet": true, "go": true,
}

var (
	snapNameRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	snapRevisionRe = regexp.MustCompile(`^--revision=[0-9]+$`)
)

// validateCommand accepts a command only when it is one devreclaim itself
// would build: an allowed tool, or the same inside a WSL distro through
// `wsl.exe -d <distro> [-u root] --`. As root, only `snap remove` is accepted.
func validateCommand(cmd []string) error {
	bad := fmt.Errorf("refused: command %q is not allowed", strings.Join(cmd, " "))

	if len(cmd) == 0 {
		return bad
	}

	if cmd[0] != "wsl.exe" {
		if !allowedCommands[cmd[0]] {
			return bad
		}

		return nil
	}

	if len(cmd) < 6 || cmd[1] != "-d" || cmd[2] == "" || strings.HasPrefix(cmd[2], "-") {
		return bad
	}

	i, root := 3, false

	if cmd[i] == "-u" {
		if cmd[i+1] != "root" {
			return bad
		}

		i, root = i+2, true
	}

	if i >= len(cmd) || cmd[i] != "--" || i+1 >= len(cmd) {
		return bad
	}

	inner := cmd[i+1:]

	if root {
		if len(inner) == 4 && inner[0] == "snap" && inner[1] == "remove" &&
			snapNameRe.MatchString(inner[2]) && snapRevisionRe.MatchString(inner[3]) {
			return nil
		}

		return bad
	}

	if !allowedCommands[inner[0]] {
		return bad
	}

	return nil
}

// Result is the outcome for one item of the plan.
type Result struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path,omitempty"`
	Action string `json:"action"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes"`
	Error  string `json:"error,omitempty"`
}

// Executor reclaims the items of a plan. Every dependency is injected so the
// tests never touch the real Recycle Bin or run real commands.
type Executor struct {
	Guard safety.Guard
	// Roots are the scan roots a project artifact must be inside.
	Roots  []string
	Mode   Mode
	Trash  func(path string) error
	Remove func(path string) error
	Runner exec.CommandRunner
	// Verify re-checks an item right before it is removed (optional).
	Verify  func(core.Candidate) error
	Journal *Journal
	Now     func() time.Time
}

// Run reclaims the items one by one. A failure on one item does not stop the
// others. It stops early only when ctx is done or the journal cannot be written.
func (e *Executor) Run(ctx context.Context, items []core.Candidate) ([]Result, error) {
	results := make([]Result, 0, len(items))

	for _, c := range items {
		if err := ctx.Err(); err != nil {
			return results, err
		}

		r := e.reclaim(ctx, c)
		results = append(results, r)

		if err := e.Journal.Write(e.entry(c, r)); err != nil {
			return results, fmt.Errorf("write journal (stopped before the next item): %w", err)
		}
	}

	return results, nil
}

func (e *Executor) reclaim(ctx context.Context, c core.Candidate) Result {
	r := Result{ID: c.ID, Label: c.Label, Path: c.Path, Action: c.Action.Type, Status: StatusDone, Bytes: c.ReclaimBytes}

	var err error

	switch c.Action.Type {
	case "delete-dir":
		err = e.deleteDir(c)
	case "run-command":
		err = e.runCommand(ctx, c)
	default:
		err = fmt.Errorf("unsupported action %q", c.Action.Type)
	}

	if err != nil {
		r.Status, r.Bytes, r.Error = StatusFailed, 0, err.Error()
	}

	return r
}

func (e *Executor) deleteDir(c core.Candidate) error {
	var roots []string

	if c.Category == core.CategoryProjectArtifacts {
		if len(e.Roots) == 0 {
			return errors.New("no scan root: refusing to delete a project artifact")
		}

		roots = e.Roots
	}

	if err := e.Guard.Check(c.Path, roots); err != nil {
		return fmt.Errorf("refused: %w", err)
	}

	if e.Verify != nil {
		if err := e.Verify(c); err != nil {
			return fmt.Errorf("changed since the scan: %w", err)
		}
	}

	remove := e.Trash
	if e.Mode == ModeDelete {
		remove = e.Remove
	}

	if remove == nil {
		return errors.New("this removal mode is not available here")
	}

	if err := remove(c.Path); err != nil {
		return err
	}

	if _, err := os.Lstat(c.Path); err == nil {
		return errors.New("the folder still exists after the operation")
	}

	return nil
}

func (e *Executor) runCommand(ctx context.Context, c core.Candidate) error {
	cmd := c.Action.Command
	if err := validateCommand(cmd); err != nil {
		return err
	}

	if e.Runner == nil {
		return errors.New("no command runner")
	}

	_, stderr, err := e.Runner.Run(ctx, cmd[0], cmd[1:]...)
	if err != nil {
		msg := strings.TrimSpace(exec.DecodeOutput(stderr))
		if msg == "" {
			return err
		}

		return fmt.Errorf("%w: %s", err, msg)
	}

	return nil
}

func (e *Executor) entry(c core.Candidate, r Result) Entry {
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}

	mode := string(e.Mode)
	if r.Action == "run-command" {
		mode = "command"
	} else if mode == "" {
		mode = string(ModeTrash)
	}

	return Entry{
		Time: now().UTC(), Category: string(c.Category), Label: c.Label, Path: c.Path,
		Action: r.Action, Mode: mode, Bytes: r.Bytes, Result: r.Status, Error: r.Error,
	}
}
