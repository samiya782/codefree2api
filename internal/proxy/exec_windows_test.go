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

func TestNpmInstallLaunchesExe(t *testing.T) {
	stub := os.Getenv("CODEFREE2API_STUB_EXE")
	if stub == "" {
		stub = filepath.Join(filepath.Dir(os.Args[0]), "stub.exe")
	}
	if st, err := os.Stat(stub); err != nil || st.IsDir() {
		t.Skip("set CODEFREE2API_STUB_EXE to a windows exe that prints its arguments")
	}
	// A space in the prefix matches installs under "Program Files".
	prefix := filepath.Join(t.TempDir(), "npm prefix")
	exe := filepath.Join(prefix, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, stub, exe)
	if err := os.WriteFile(filepath.Join(prefix, "codefree-o.cmd"), []byte(npmGlobalCmd()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefix, "codefree-o"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", prefix)
	t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
	t.Setenv("NoDefaultCurrentDirectoryInExePath", "1")

	got, err := resolveExecutable("codefree-o")
	if err != nil {
		t.Fatal(err)
	}
	sameFile(t, got, exe)
	args := []string{"serve", "--pure", "--hostname", "127.0.0.1", "--port", "0"}
	if out := runArgs(t, got, args); out != strings.Join(args, "|") {
		t.Fatalf("direct exe: %q", out)
	}
	// Fallback: run the npm .cmd itself, which expands %dp0% and starts the exe.
	if out := runArgs(t, filepath.Join(prefix, "codefree-o.cmd"), args); out != strings.Join(args, "|") {
		t.Fatalf("npm cmd: %q", out)
	}

	localExe := filepath.Join(prefix, "project", "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	if err := os.MkdirAll(filepath.Dir(localExe), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFile(t, stub, localExe)
	localCmd := filepath.Join(prefix, "project", "node_modules", ".bin", "codefree-o.cmd")
	if err := os.MkdirAll(filepath.Dir(localCmd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localCmd, []byte(npmLocalCmd()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveExecutable(localCmd)
	if err != nil {
		t.Fatal(err)
	}
	sameFile(t, got, localExe)
	if out := runArgs(t, localCmd, args); out != strings.Join(args, "|") {
		t.Fatalf("local cmd: %q", out)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	body, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, body, 0o755); err != nil {
		t.Fatal(err)
	}
}

func sameFile(t *testing.T, got, want string) {
	t.Helper()
	a, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(a, b) {
		t.Fatalf("got %s want %s", got, want)
	}
}

func runArgs(t *testing.T, path string, args []string) string {
	t.Helper()
	cmd := execCommandContext(context.Background(), path, args...)
	isolateProcess(cmd)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: %v %q", path, err, out)
	}
	return strings.TrimSpace(string(out))
}
