//go:build windows

package proxy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdRunsBatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ok.cmd")
	body := "@echo off\r\necho ok-from-cmd\r\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := execCommandContext(context.Background(), path)
	isolateProcess(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%v %q", err, out)
	}
	if strings.TrimSpace(string(out)) != "ok-from-cmd" {
		t.Fatalf("got %q", out)
	}
}
