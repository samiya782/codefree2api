package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCmdFindsPackageExe(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(dir, "codefree-o.cmd")
	if err := os.WriteFile(cmd, []byte("shim"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutable(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("got %s want %s", got, exe)
	}
}

func TestBatchExePathExpandsDp0(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	cmd := filepath.Join(dir, "codefree-o.cmd")
	text := `@"%~dp0\node_modules\@srdcloud\codefree-o\bin\codefree-o.exe" %*`
	got := batchExePath(cmd, text)
	if got != exe {
		t.Fatalf("got %s want %s", got, exe)
	}
}

func TestBatchExePathReadsNpmShim(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	cmd := filepath.Join(dir, "codefree-o.cmd")
	// npm cmd-shim writes this when the bin target has no shebang.
	text := "@ECHO off\r\n" +
		"GOTO start\r\n" +
		":find_dp0\r\n" +
		"SET dp0=%~dp0\r\n" +
		"EXIT /b\r\n" +
		":start\r\n" +
		"SETLOCAL\r\n" +
		"CALL :find_dp0\r\n" +
		`"%dp0%\node_modules\@srdcloud\codefree-o\bin\codefree-o.exe"   %*` + "\r\n"
	got := batchExePath(cmd, text)
	if got != exe {
		t.Fatalf("got %s want %s", got, exe)
	}
}

func TestResolveVoltaShim(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "tools", "image", "packages", "@srdcloud", "codefree-o", "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(root, "bin", "codefree-o.cmd")
	if err := os.MkdirAll(filepath.Dir(cmd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmd, []byte("@echo off\r\nvolta run %~n0 %*\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutable(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("got %s want %s", got, exe)
	}
}

func TestResolveBinShim(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "node_modules", ".bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := filepath.Join(binDir, "codefree-o.cmd")
	if err := os.WriteFile(cmd, []byte("shim"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutable(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("cmd: got %s want %s", got, exe)
	}
	shim := filepath.Join(binDir, "codefree-o")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = resolveExecutable(shim)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("shim: got %s want %s", got, exe)
	}
}

func TestResolveKeepsExeAndFallsBackToCmd(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "codefree-o.exe")
	if err := os.WriteFile(exe, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveExecutable(exe)
	if err != nil {
		t.Fatal(err)
	}
	if got != exe {
		t.Fatalf("exe: got %s want %s", got, exe)
	}
	cmd := filepath.Join(dir, "missing", "codefree-o.cmd")
	if err := os.MkdirAll(filepath.Dir(cmd), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cmd, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = resolveExecutable(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if got != cmd {
		t.Fatalf("fallback: got %s want %s", got, cmd)
	}
}

func TestFileURL(t *testing.T) {
	if got := fileURL(`C:\Users\a\b.png`); got != "file:///C:/Users/a/b.png" {
		t.Fatalf("drive: %s", got)
	}
	if got := fileURL(`\\host\share\a.png`); got != "file://host/share/a.png" {
		t.Fatalf("unc: %s", got)
	}
	dir := t.TempDir()
	got := fileURL(dir)
	if !strings.HasPrefix(got, "file:///") {
		t.Fatalf("local: %s", got)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("backslash in url: %s", got)
	}
}

func TestCmdLineQuotesSpaces(t *testing.T) {
	got := cmdLine(`C:\Program Files\codefree-o.cmd`, []string{"serve", "--hostname", "127.0.0.1"})
	inner := `"C:\Program Files\codefree-o.cmd" serve --hostname 127.0.0.1`
	want := `"` + inner + `"`
	if got != want {
		t.Fatalf("got %s", got)
	}
}
