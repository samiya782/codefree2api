package lines

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func insideWorkTree(ctx context.Context, dir string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return false, fmt.Errorf("git 不可用")
		}
		return false, nil
	}
	return strings.TrimSpace(string(out)) == "true", nil
}

func remoteURLs(ctx context.Context, dir string) ([]string, error) {
	ok, err := insideWorkTree(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errNotRepo
	}
	namesOut, err := git(ctx, dir, "remote")
	if err != nil {
		return nil, errNoRemote
	}
	seen := map[string]struct{}{}
	var urls []string
	for _, name := range strings.Fields(namesOut) {
		url, err := git(ctx, dir, "remote", "get-url", name)
		if err != nil {
			return nil, errNoRemote
		}
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil, errNoRemote
	}
	return urls, nil
}
