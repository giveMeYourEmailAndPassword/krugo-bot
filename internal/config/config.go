package config

import (
	"fmt"
	"os"
	"path/filepath"
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
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		TelegramToken: os.Getenv("TELEGRAM_BOT_TOKEN"),
		OpenAIKey:     os.Getenv("OPENAI_API_KEY"),
		OpenAIBaseURL: os.Getenv("OPENAI_BASE_URL"),
		AIModel:       envOrDefault("AI_MODEL", "gpt-4o-mini"),
		DatabasePath:  envOrDefault("DATABASE_PATH", "/app/data/hermes.db"),
		BotEnv:        envOrDefault("BOT_ENV", "development"),
	}

	if cfg.TelegramToken == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	if cfg.OpenAIBaseURL == "" {
		cfg.OpenAIBaseURL = "https://api.openai.com/v1"
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
