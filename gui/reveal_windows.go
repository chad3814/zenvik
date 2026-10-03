package main

import (
	"os/exec"
	"syscall"
)

// setCmdLine gives Explorer its command line verbatim, so the quotes stay
// around the path only.
func setCmdLine(cmd *exec.Cmd, goos, path string) {
	if goos == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: explorerCmdLine(path)}
	}
}
