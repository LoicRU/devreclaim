// Package wslcompact shrinks the virtual disk (ext4.vhdx) of a WSL2 distro.
// It stops WSL, so everything that touches the machine goes through injected
// functions and is tested with fakes.
package wslcompact

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoicRU/devreclaim/internal/platform/exec"
	"github.com/LoicRU/devreclaim/internal/scanners/wsl"
)

// Method is how the virtual disk is compacted.
type Method string

// Supported methods.
const (
	MethodAuto     Method = "auto"
	MethodOptimize Method = "optimize-vhd"
	MethodDiskpart Method = "diskpart"
)

// lowGainPercent is the gain below which the result is flagged as suspicious.
const lowGainPercent = 5

// ErrNeedsAdmin means compaction must run from an elevated terminal.
var ErrNeedsAdmin = errors.New("administrator rights are required: reopen the terminal as administrator")

// Compactor holds everything that touches the machine.
type Compactor struct {
	Runner    exec.CommandRunner
	FileSize  func(path string) (int64, error)
	FreeSpace func(path string) (int64, error)
	IsAdmin   func() bool
	IsSparse  func(path string) bool
	// WriteScript saves a diskpart script and returns its path and a cleanup.
	WriteScript func(content string) (path string, cleanup func(), err error)
	Sleep       func(time.Duration)
	// IsLocked reports whether another process still holds the file open.
	IsLocked func(path string) bool
}

// Plan is what compaction will do for one distro.
type Plan struct {
	Distro     wsl.Distro `json:"-"`
	Name       string     `json:"distro"`
	VHDX       string     `json:"vhdx"`
	SizeBefore int64      `json:"sizeBefore"`
	Method     Method     `json:"method"`
	Steps      []string   `json:"steps"`
	Warnings   []string   `json:"warnings"`
}

// Result is what compaction measured.
type Result struct {
	Name     string   `json:"distro"`
	Before   int64    `json:"before"`
	After    int64    `json:"after"`
	Gain     int64    `json:"gain"`
	LowGain  bool     `json:"lowGain"`
	Method   Method   `json:"method"`
	Warnings []string `json:"warnings"`
}

// Prepare checks that the distro can be compacted and describes the steps.
// It changes nothing and does not need administrator rights.
func (c *Compactor) Prepare(ctx context.Context, d wsl.Distro, method Method) (Plan, error) {
	if strings.HasPrefix(d.Name, "docker-desktop") {
		return Plan{}, fmt.Errorf("%s is managed by Docker Desktop: compact it from Docker Desktop", d.Name)
	}

	if d.Version != 2 {
		return Plan{}, fmt.Errorf("%s is a WSL1 distro: it has no virtual disk to compact", d.Name)
	}

	p := Plan{Distro: d, Name: d.Name, VHDX: d.VHDXPath(), Warnings: []string{}}

	size, err := c.FileSize(p.VHDX)
	if err != nil {
		return Plan{}, fmt.Errorf("cannot read the virtual disk: %w", err)
	}

	p.SizeBefore = size

	if err = c.checkFreeSpace(&p); err != nil {
		return Plan{}, err
	}

	if c.IsSparse != nil && c.IsSparse(p.VHDX) {
		return Plan{}, fmt.Errorf("the virtual disk of %s is sparse: it already shrinks by itself and "+
			"Optimize-VHD does not work on it; to compact it anyway, turn sparse off yourself with "+
			"`wsl --manage %s --set-sparse false`", d.Name, d.Name)
	}

	if p.Method, err = c.resolveMethod(ctx, method); err != nil {
		return Plan{}, err
	}

	if p.Method == MethodDiskpart && !isASCII(p.VHDX) {
		return Plan{}, fmt.Errorf("diskpart cannot read the path %q (non-ASCII characters): use --method optimize-vhd", p.VHDX)
	}

	p.Steps = []string{
		"fstrim -av inside " + d.Name + " (free blocks, a failure only warns)",
		"wsl --shutdown (stops every WSL distro)",
		"compact " + p.VHDX + " with " + string(p.Method),
		"measure the size before and after",
	}

	return p, nil
}

func (c *Compactor) checkFreeSpace(p *Plan) error {
	free, err := c.FreeSpace(p.VHDX)
	if err != nil {
		p.Warnings = append(p.Warnings, fmt.Sprintf("cannot read the free disk space: %v", err))

		return nil
	}

	need := max(int64(1<<30), p.SizeBefore/10)
	if free < need {
		return fmt.Errorf("not enough free disk space for compaction: %d MB free, at least %d MB needed",
			free>>20, need>>20)
	}

	return nil
}

func (c *Compactor) resolveMethod(ctx context.Context, m Method) (Method, error) {
	switch m {
	case MethodOptimize, MethodDiskpart:
		return m, nil
	case MethodAuto, "":
		out, _, err := c.Runner.Run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
			"if (Get-Command Optimize-VHD -ErrorAction SilentlyContinue) { 'yes' } else { 'no' }")
		if err == nil && strings.Contains(strings.ToLower(exec.DecodeOutput(out)), "yes") {
			return MethodOptimize, nil
		}

		return MethodDiskpart, nil
	default:
		return "", fmt.Errorf("unknown method %q (use auto, optimize-vhd or diskpart)", m)
	}
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}

	return true
}

// Run compacts the disk. progress receives a line per step.
func (c *Compactor) Run(ctx context.Context, p Plan, progress func(string)) (Result, error) {
	res := Result{Name: p.Name, Method: p.Method, Warnings: []string{}}

	if !c.IsAdmin() {
		return res, ErrNeedsAdmin
	}

	progress("fstrim inside " + p.Name)

	if _, stderr, err := c.Runner.Run(ctx, "wsl.exe", "-d", p.Name, "-u", "root", "--", "fstrim", "-av"); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"fstrim failed (%v %s): less space may be recovered", err, strings.TrimSpace(exec.DecodeOutput(stderr))))
	}

	progress("wsl --shutdown")

	if _, stderr, err := c.Runner.Run(ctx, "wsl.exe", "--shutdown"); err != nil {
		return res, fmt.Errorf("wsl --shutdown failed: %w: %s", err, strings.TrimSpace(exec.DecodeOutput(stderr)))
	}

	if err := c.waitUnlocked(p.VHDX); err != nil {
		return res, err
	}

	before, err := c.FileSize(p.VHDX)
	if err != nil {
		return res, fmt.Errorf("cannot read the virtual disk: %w", err)
	}

	res.Before = before

	progress("compacting with " + string(p.Method) + " (this can take several minutes)")

	if p.Method == MethodDiskpart {
		err = c.diskpart(ctx, p.VHDX)
	} else {
		err = c.optimize(ctx, p.VHDX)
	}

	if err != nil {
		return res, err
	}

	after, err := c.FileSize(p.VHDX)
	if err != nil {
		return res, fmt.Errorf("cannot read the virtual disk after compaction: %w", err)
	}

	res.After = after
	res.Gain = max(before-after, 0)
	res.LowGain = res.Gain*100 < before*lowGainPercent

	return res, nil
}

const (
	unlockPoll    = 2 * time.Second
	unlockTimeout = 60 * time.Second
)

// waitUnlocked waits until Windows releases the disk after wsl --shutdown:
// the file stays locked for a few seconds, and compacting too early fails.
func (c *Compactor) waitUnlocked(vhdx string) error {
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}

	if c.IsLocked == nil {
		sleep(unlockPoll)

		return nil
	}

	for waited := time.Duration(0); c.IsLocked(vhdx); waited += unlockPoll {
		if waited >= unlockTimeout {
			return fmt.Errorf("the virtual disk is still in use %d seconds after wsl --shutdown: "+
				"close what uses WSL (VS Code, Docker Desktop) and try again", int(unlockTimeout.Seconds()))
		}

		sleep(unlockPoll)
	}

	return nil
}

func quoteSingle(s string) string { return strings.ReplaceAll(s, "'", "''") }

func (c *Compactor) optimize(ctx context.Context, vhdx string) error {
	script := fmt.Sprintf("Optimize-VHD -Path '%s' -Mode Full -ErrorAction Stop", quoteSingle(vhdx))

	_, stderr, err := c.Runner.Run(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	if err != nil {
		return fmt.Errorf("Optimize-VHD failed (a sparse disk is a common cause): %w: %s",
			err, strings.TrimSpace(exec.DecodeOutput(stderr)))
	}

	return nil
}

func diskpartScript(vhdx string) string {
	return fmt.Sprintf("select vdisk file=\"%s\"\r\nattach vdisk readonly\r\ncompact vdisk\r\ndetach vdisk\r\n", vhdx)
}

func detachScript(vhdx string) string {
	return fmt.Sprintf("select vdisk file=\"%s\"\r\ndetach vdisk\r\n", vhdx)
}

// diskpart compacts the disk and always tries to detach it afterwards, even
// when compaction failed, so the file is never left locked.
func (c *Compactor) diskpart(ctx context.Context, vhdx string) error {
	defer c.runScript(ctx, detachScript(vhdx)) //nolint:errcheck // best effort cleanup

	return c.runScript(ctx, diskpartScript(vhdx))
}

func (c *Compactor) runScript(ctx context.Context, content string) error {
	path, cleanup, err := c.WriteScript(content)
	if err != nil {
		return fmt.Errorf("write the diskpart script: %w", err)
	}
	defer cleanup()

	stdout, stderr, err := c.Runner.Run(ctx, "diskpart.exe", "/s", path)
	if err != nil {
		return fmt.Errorf("diskpart failed: %w: %s %s", err,
			strings.TrimSpace(string(stdout)), strings.TrimSpace(string(stderr)))
	}

	return nil
}
