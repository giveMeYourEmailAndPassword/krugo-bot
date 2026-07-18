package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken string
	OpenAIKey     string
	OpenAIBaseURL string
	AIModel       string
	DatabasePath  string
	BotEnv        string
	AllowedUsers  []int64
}

// Load reads configuration from environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		OpenAIKey:     os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: envOrDefault("OPENAI_BASE_URL", "https://api.deepseek.com"),
		AIModel:       envOrDefault("AI_MODEL", "deepseek-v4-flash"),
		DatabasePath:  envOrDefault("DATABASE_PATH", "/app/data/hermes.db"),
		BotEnv:        envOrDefault("BOT_ENV", "development"),
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.OpenAIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY is required")
	}

	raw := os.Getenv("TELEGRAM_ALLOWED_USERS")
	if raw == "" {
		return nil, fmt.Errorf("TELEGRAM_ALLOWED_USERS is required")
	}
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid TELEGRAM_ALLOWED_USERS value: %q", s)
		}
		cfg.AllowedUsers = append(cfg.AllowedUsers, id)
	}

	cfg.OpenAIBaseURL = strings.TrimRight(cfg.OpenAIBaseURL, "/")

	return cfg, nil
}

// DatabaseDir returns the parent directory of the database file,
// ensuring it exists on disk.
func (c *Config) DatabaseDir() string {
	return filepath.Dir(c.DatabasePath)
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
