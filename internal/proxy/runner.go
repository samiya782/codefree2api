package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

type Runner struct {
	Bin     string
	WorkDir string
}

func childEnv() []string {
	env := os.Environ()
	out := make([]string, 0, len(env)+2)
	for _, e := range env {
		if strings.HasPrefix(e, "FORCE_COLOR=") || strings.HasPrefix(e, "NO_COLOR=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "NO_COLOR=1", "TERM=dumb")
	return out
}

func (r *Runner) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.Env = childEnv()
	if r.WorkDir != "" {
		cmd.Dir = r.WorkDir
	}
	return cmd
}

func (r *Runner) ListModels(ctx context.Context) ([]string, error) {
	cmd := r.command(ctx, "models", "--pure")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(StripANSI(stderr.String()))
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("codefree-o models --pure failed: %s", msg)
	}
	return ParseModelLines(string(out)), nil
}

// Run executes: codefree-o run --pure --model <model>
// The prompt is written to stdin. onChunk, if set, receives UTF-8-safe stdout pieces as they arrive.
func (r *Runner) Run(ctx context.Context, model, prompt string, onChunk func(string) error) (string, error) {
	cmd := r.command(ctx, "run", "--pure", "--model", model)
	cmd.Stdin = strings.NewReader(prompt)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start codefree-o: %w", err)
	}

	var collected strings.Builder
	if err := readUTF8Chunks(stdout, func(s string) error {
		collected.WriteString(s)
		if onChunk != nil {
			return onChunk(s)
		}
		return nil
	}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return collected.String(), err
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return collected.String(), fmt.Errorf("codefree-o timed out or canceled")
		}
		msg := strings.TrimSpace(StripANSI(stderr.String()))
		if len(msg) > 2000 {
			msg = msg[:2000] + "..."
		}
		if msg == "" {
			msg = err.Error()
		}
		return collected.String(), fmt.Errorf("codefree-o run failed: %s", msg)
	}
	return collected.String(), nil
}

func readUTF8Chunks(r io.Reader, emit func(string) error) error {
	buf := make([]byte, 1024)
	var pending []byte
	for {
		n, err := r.Read(buf)
		if n > 0 {
			data := append(pending, buf[:n]...)
			valid, rest := splitValidUTF8(data)
			pending = rest
			if len(valid) > 0 {
				if e := emit(string(valid)); e != nil {
					return e
				}
			}
		}
		if err == io.EOF {
			if len(pending) > 0 {
				if e := emit(string(pending)); e != nil {
					return e
				}
			}
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func splitValidUTF8(b []byte) (valid, rest []byte) {
	if len(b) == 0 || utf8.Valid(b) {
		return b, nil
	}
	for i := 1; i <= 3 && i < len(b); i++ {
		cut := len(b) - i
		if utf8.Valid(b[:cut]) {
			return b[:cut], b[cut:]
		}
	}
	return b, nil
}
