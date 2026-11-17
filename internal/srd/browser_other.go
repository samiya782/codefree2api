//go:build !windows

package srd

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
