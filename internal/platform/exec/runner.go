// Package exec runs external commands behind an interface so that scanners can
// be tested without touching the real machine.
package exec

import (
	"bytes"
	"context"
	osexec "os/exec"
)

// CommandRunner runs an external command and returns what it wrote.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// OSRunner runs real commands on the host.
type OSRunner struct{}

// Run executes name with args and waits for it to finish.
func (OSRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer

	//nolint:gosec // callers pass fixed command names, never user input
	cmd := osexec.CommandContext(ctx, name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	detachConsole(cmd)

	err := cmd.Run()

	return stdout.Bytes(), stderr.Bytes(), err
}
