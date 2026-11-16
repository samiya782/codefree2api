package proxy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ExecCommand starts path. On Windows a .cmd or .bat is run through cmd.exe.
func ExecCommand(path string, args ...string) *exec.Cmd {
	return execCommandContext(context.Background(), path, args...)
}

// resolveExecutable finds the codefree-o binary. A bare name is looked up on
// PATH. An npm Windows shim (codefree-o.cmd, or the extensionless script
// beside it) is followed to codefree-o.exe. The .cmd is returned only when
// that exe cannot be found; Windows then runs it through cmd.exe.
func resolveExecutable(bin string) (string, error) {
	bin = strings.TrimSpace(bin)
	if bin == "" {
		bin = DefaultBin
	}
	path := bin
	if !strings.ContainsAny(bin, `/\`) {
		found, err := exec.LookPath(bin)
		if err != nil {
			return "", fmt.Errorf("%s not found on PATH", bin)
		}
		path = found
	}
	if strings.EqualFold(filepath.Ext(path), ".exe") {
		return path, nil
	}
	if exe, ok := exeNearBatch(path); ok {
		return exe, nil
	}
	script := path
	if !isBatch(script) {
		sibling := path + ".cmd"
		if st, err := os.Stat(sibling); err == nil && !st.IsDir() {
			script = sibling
		}
	}
	if isBatch(script) {
		if exe, ok := exeInBatch(script); ok {
			return exe, nil
		}
		return script, nil
	}
	return path, nil
}

func isBatch(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

func exeNearBatch(cmdPath string) (string, bool) {
	dir := filepath.Dir(cmdPath)
	candidates := []string{
		// npm prefix: %APPDATA%\npm\codefree-o.cmd
		filepath.Join(dir, "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe"),
		// npm local: node_modules/.bin/codefree-o.cmd
		filepath.Join(dir, "..", "@srdcloud", "codefree-o", "bin", "codefree-o.exe"),
		// Volta: %LOCALAPPDATA%\Volta\bin\codefree-o.cmd
		filepath.Join(dir, "..", "tools", "image", "packages", "@srdcloud", "codefree-o", "node_modules", "@srdcloud", "codefree-o", "bin", "codefree-o.exe"),
		filepath.Join(dir, "codefree-o.exe"),
	}
	for _, c := range candidates {
		st, err := os.Stat(c)
		if err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}

func exeInBatch(cmdPath string) (string, bool) {
	body, err := os.ReadFile(cmdPath)
	if err != nil {
		return "", false
	}
	raw := batchExePath(cmdPath, string(body))
	if raw == "" {
		return "", false
	}
	st, err := os.Stat(raw)
	if err != nil || st.IsDir() {
		return "", false
	}
	return raw, true
}

// batchExePath pulls a codefree-o.exe path out of an npm cmd shim.
// %~dp0 and %dp0% expand to the shim directory. npm's cmd-shim writes %dp0%.
func batchExePath(cmdPath, text string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, "codefree-o.exe")
	if idx < 0 {
		return ""
	}
	start := idx
	for start > 0 {
		c := text[start-1]
		if c == '"' || c == '\'' || c == ' ' || c == '=' || c == '\n' || c == '\r' || c == '\t' {
			break
		}
		start--
	}
	raw := strings.Trim(text[start:idx+len("codefree-o.exe")], `"'`)
	dir := filepath.Dir(cmdPath) + string(filepath.Separator)
	raw = expandBatchDir(raw, dir)
	raw = strings.ReplaceAll(raw, `\`, string(filepath.Separator))
	return filepath.Clean(raw)
}

func expandBatchDir(raw, dir string) string {
	lower := strings.ToLower(raw)
	var b strings.Builder
	b.Grow(len(raw) + len(dir))
	for i := 0; i < len(raw); {
		switch {
		case strings.HasPrefix(lower[i:], "%~dp0"):
			b.WriteString(dir)
			i += len("%~dp0")
		case strings.HasPrefix(lower[i:], "%dp0%"):
			b.WriteString(dir)
			i += len("%dp0%")
		default:
			b.WriteByte(raw[i])
			i++
		}
	}
	return b.String()
}

// fileURL returns a file URL codefree-o accepts on both Windows and Unix.
func fileURL(path string) string {
	if u, ok := windowsFileURL(path); ok {
		return u
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if u, ok := windowsFileURL(path); ok {
		return u
	}
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return "file://" + slash
}

func windowsFileURL(path string) (string, bool) {
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return "file:///" + strings.ReplaceAll(path, `\`, `/`), true
	}
	if strings.HasPrefix(path, `\\`) && !strings.HasPrefix(path, `\\?\`) {
		return "file:" + strings.ReplaceAll(path, `\`, `/`), true
	}
	return "", false
}

// cmdLine is the quoted command string passed to cmd.exe /d /s /c.
func cmdLine(path string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, quoteCmdArg(path))
	for _, a := range args {
		parts = append(parts, quoteCmdArg(a))
	}
	return `"` + strings.Join(parts, " ") + `"`
}

func quoteCmdArg(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"&|<>^%") {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
