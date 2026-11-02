package proxy

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
