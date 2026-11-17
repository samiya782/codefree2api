//go:build windows

package srd

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

func EditorVersion(ctx context.Context) (string, error) {
	path, err := exec.LookPath("code")
	if err != nil {
		return "", err
	}
	var out []byte
	if !isBatch(path) {
		out, err = exec.CommandContext(ctx, path, "--version").Output()
	} else {
		out, err = batchVersion(ctx, path)
	}
	if err != nil {
		return "", err
	}
	found := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(string(out))
	if found == "" {
		return "", errVersion
	}
	return found, nil
}

func batchVersion(ctx context.Context, path string) ([]byte, error) {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		comspec = root + `\System32\cmd.exe`
	}
	cmd := exec.CommandContext(ctx, comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: quote(comspec) + " /d /s /c " + quote(path) + " --version",
	}
	return cmd.Output()
}

func isBatch(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".cmd" || ext == ".bat"
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
