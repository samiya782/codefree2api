package proxy

import (
	"fmt"
	"strings"
)

// LaunchSpec is the child process started by `codefree2api launch`.
type LaunchSpec struct {
	Name string
	Args []string
	Env  []string
}

// LaunchCommand builds the environment that points claude or codex at this proxy.
func LaunchCommand(tool string, port int, password, model string, extra []string) (LaunchSpec, error) {
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	key := password
	if key == "" {
		key = "local"
	}
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "claude":
		args := []string{}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, extra...)
		return LaunchSpec{
			Name: "claude",
			Args: args,
			Env: []string{
				"ANTHROPIC_BASE_URL=" + base,
				"ANTHROPIC_API_KEY=" + key,
			},
		}, nil
	case "codex":
		args := []string{
			"-c", "model_provider=codefree2api",
			"-c", "model_providers.codefree2api.base_url=" + base + "/v1",
			"-c", "model_providers.codefree2api.wire_api=responses",
			"-c", "model_providers.codefree2api.requires_openai_auth=true",
			"-c", "model_providers.codefree2api.env_key=CODEFREE2API_OPENAI_API_KEY",
		}
		if model != "" {
			args = append(args, "-c", "model="+model)
		}
		args = append(args, extra...)
		return LaunchSpec{
			Name: "codex",
			Args: args,
			Env:  []string{"CODEFREE2API_OPENAI_API_KEY=" + key},
		}, nil
	default:
		return LaunchSpec{}, fmt.Errorf("launch supports claude and codex")
	}
}
