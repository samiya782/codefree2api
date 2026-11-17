package srd

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ListRemotes returns the remote URLs of dir. Any failure yields nil.
// An empty result does not mean the directory is not a repository.
func ListRemotes(ctx context.Context, dir string) []string {
	if dir == "" {
		return nil
	}
	names, err := gitOutput(ctx, dir, "remote")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var urls []string
	for _, name := range strings.Fields(names) {
		raw, err := gitOutput(ctx, dir, "remote", "get-url", name)
		if err != nil {
			continue
		}
		url := strings.TrimSpace(raw)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		urls = append(urls, url)
	}
	return urls
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// FirstGitURL is the first url in .git/config, walking parents from dir.
func FirstGitURL(dir string) string {
	if dir == "" {
		return ""
	}
	dir = filepath.Clean(dir)
	for {
		gitPath := filepath.Join(dir, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			config := filepath.Join(gitPath, "config")
			if !info.IsDir() {
				if raw, err := os.ReadFile(gitPath); err == nil {
					if gitdir, ok := gitdirLine(string(raw)); ok {
						if !filepath.IsAbs(gitdir) {
							gitdir = filepath.Join(dir, gitdir)
						}
						config = filepath.Join(gitdir, "config")
					}
				}
			}
			if url := firstConfigURL(config); url != "" {
				return url
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func gitdirLine(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "gitdir:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "gitdir:")), true
		}
	}
	return "", false
}

func firstConfigURL(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		text := strings.TrimSpace(string(line))
		if !strings.HasPrefix(text, "url") {
			continue
		}
		name, value, ok := strings.Cut(text, "=")
		if !ok || strings.TrimSpace(name) != "url" {
			continue
		}
		if url := strings.TrimSpace(value); url != "" {
			return url
		}
	}
	return ""
}
