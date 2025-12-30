
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	BotToken        string  `json:"bot_token"`
	DataDir         string  `json:"data_dir"`
	InitialAdminIDs []int64 `json:"initial_admin_ids,omitempty"`

	// Seed credentials (optional). These are copied into DB on first boot if DB doesn't have them.
	BonbastAPIUsername string `json:"bonbast_api_username,omitempty"`
	BonbastAPIHash     string `json:"bonbast_api_hash,omitempty"`
	NavasanAPIKey      string `json:"navasan_api_key,omitempty"`

	// If true, bot will log debug messages.
	Debug bool `json:"debug,omitempty"`
}

func DefaultDataDir() string {
	if v := os.Getenv("PCB_DATA_DIR"); v != "" {
		return v
	}
	// Preferred system path
	return "/var/lib/persian-currency-bot"
}

func DefaultConfigPath() string {
	if v := os.Getenv("PCB_CONFIG"); v != "" {
		return v
	}
	// Preferred system path
	return "/etc/persian-currency-bot/config.json"
}

func Load(path string) (Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	var cfg Config
	// 1) Try file
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return Config{}, fmt.Errorf("invalid config json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	// 2) Env fallback / override
	if v := os.Getenv("BOT_TOKEN"); v != "" {
		cfg.BotToken = v
	}
	if v := os.Getenv("PCB_BOT_TOKEN"); v != "" {
		cfg.BotToken = v
	}
	if v := os.Getenv("DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("PCB_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("PCB_DEBUG"); v != "" {
		cfg.Debug = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := os.Getenv("PCB_INITIAL_ADMINS"); v != "" && len(cfg.InitialAdminIDs) == 0 {
		cfg.InitialAdminIDs = parseIDList(v)
	}

	// Defaults
	if cfg.DataDir == "" {
		cfg.DataDir = DefaultDataDir()
	}
	cfg.DataDir = filepath.Clean(cfg.DataDir)

	if cfg.BotToken == "" {
		return Config{}, fmt.Errorf("missing bot_token (set in %s or BOT_TOKEN env)", path)
	}
	return cfg, nil
}

func parseIDList(s string) []int64 {
	var out []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		id, err := strconv.ParseInt(part, 10, 64)
		if err == nil {
			out = append(out, id)
		}
	}
	return out
}
