//go:build unix

package proxy

import (
	"context"
	"os/exec"
	"syscall"
)

func execCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, path, args...)
}

func isolateProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		_ = cmd.Process.Kill()
	}
}
