//go:build !windows

package exec

import osexec "os/exec"

func detachConsole(*osexec.Cmd) {}
