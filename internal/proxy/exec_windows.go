//go:build windows

package proxy

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

func execCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	if !isBatch(path) {
		return exec.CommandContext(ctx, path, args...)
	}
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		comspec = root + `\System32\cmd.exe`
	}
	// Go quotes arguments again. A pre-quoted /c string then becomes the program
	// name. CmdLine is passed to CreateProcess unchanged.
	cmd := exec.CommandContext(ctx, comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: quoteCmdArg(comspec) + " /d /s /c " + cmdLine(path, args),
	}
	return cmd
}

// createNoWindow is Windows CREATE_NO_WINDOW (0x08000000). The standard
// syscall package exports CREATE_NEW_PROCESS_GROUP but not this flag.
const createNoWindow = 0x08000000

func isolateProcess(cmd *exec.Cmd) {
	attr := cmd.SysProcAttr
	if attr == nil {
		attr = &syscall.SysProcAttr{}
	}
	attr.CreationFlags |= syscall.CREATE_NEW_PROCESS_GROUP | createNoWindow
	attr.HideWindow = true
	cmd.SysProcAttr = attr
}

func killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	killer := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
	killer.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: createNoWindow,
		HideWindow:    true,
	}
	_ = killer.Run()
	_ = cmd.Process.Kill()
}
