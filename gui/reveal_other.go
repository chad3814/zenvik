//go:build !windows

package main

import "os/exec"

// setCmdLine does nothing outside Windows: the arguments are passed as they are.
func setCmdLine(*exec.Cmd, string, string) {}
