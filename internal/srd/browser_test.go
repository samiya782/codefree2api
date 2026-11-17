package srd

import (
	"runtime"
	"strings"
	"testing"
)

func TestWSLBrowserPrintsThePage(t *testing.T) {
	if runtime.GOOS == "windows" || wslPowerShell() == "" {
		t.Skip()
	}
	cmd, show := browserCommand("https://example.com/login?a=1&b=2")
	if !show {
		t.Fatal("show")
	}
	got := strings.Join(cmd.Args, " ")
	if !strings.Contains(got, "Start-Process") || !strings.Contains(got, "a=1&b=2") || strings.Contains(got, "cmd.exe") {
		t.Fatal("command")
	}
}
