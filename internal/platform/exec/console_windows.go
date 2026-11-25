//go:build windows

package exec

import (
	osexec "os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// detachConsole starts the child without a console. Otherwise tools such as
// wsl.exe, run in parallel, change the mode of our terminal and break its
// output (lines shifted to the right).
func detachConsole(cmd *osexec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
