package srd

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// errShowURL tells Login to print the page address. WSL's start command
// returns success even when the Windows browser never appears.
var errShowURL = errors.New("登录页见标准错误")

// OpenBrowser starts the OS browser and does not wait for it to exit.
// Windows uses the file protocol handler. WSL asks PowerShell to open the
// Windows browser and still reports errShowURL so the address is printed.
func OpenBrowser(raw string) error {
	cmd, showURL := browserCommand(raw)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if showURL {
		return errShowURL
	}
	return nil
}

func browserCommand(raw string) (*exec.Cmd, bool) {
	switch {
	case runtime.GOOS == "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", raw), false
	case wslPowerShell() != "":
		quoted := strings.ReplaceAll(raw, "'", "''")
		return exec.Command(wslPowerShell(), "-NoProfile", "-WindowStyle", "Hidden", "-Command", "Start-Process '"+quoted+"'"), true
	default:
		return exec.Command("xdg-open", raw), false
	}
}

func wslPowerShell() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	p := "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	if found, err := exec.LookPath("powershell.exe"); err == nil {
		return found
	}
	return ""
}
