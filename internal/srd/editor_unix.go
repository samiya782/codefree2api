//go:build unix

package srd

import (
	"context"
	"os/exec"
	"regexp"
)

func EditorVersion(ctx context.Context) (string, error) {
	path, err := exec.LookPath("code")
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", err
	}
	found := regexp.MustCompile(`\d+\.\d+\.\d+`).FindString(string(out))
	if found == "" {
		return "", errVersion
	}
	return found, nil
}
