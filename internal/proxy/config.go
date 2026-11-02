package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const (
	DefaultPort = 8000
	DefaultBin  = "/home/samiya/.local/share/mise/installs/node/24.22.0/bin/codefree-o"
	runTimeout  = 10 * time.Minute
	listTimeout = 45 * time.Second
)

// Config is the optional JSON file loaded by -config.
type Config struct {
	Port         int               `json:"port"`
	Password     string            `json:"password"`
	Bin          string            `json:"bin"`
	ModelAliases map[string]string `json:"model_aliases"`
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
