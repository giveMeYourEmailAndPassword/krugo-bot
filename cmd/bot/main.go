package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/amantur/krugo-bot/internal/config"
	"github.com/amantur/krugo-bot/internal/hermes"
	"github.com/amantur/krugo-bot/internal/logger"
	"github.com/amantur/krugo-bot/internal/storage"
	"github.com/amantur/krugo-bot/internal/telegram"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	log := logger.New(cfg.BotEnv)

	// Ensure database directory exists.
	if err := os.MkdirAll(cfg.DatabaseDir(), 0o755); err != nil {
		log.Error("create data dir", "error", err)
		os.Exit(1)
	}

	store, err := storage.NewSQLiteStore(cfg.DatabasePath)
	if err != nil {
		log.Error("open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()
	log.Info("database ready", "path", cfg.DatabasePath)

	// Telegram bot setup.
	pref := telebot.Settings{
		Token:  cfg.TelegramToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
		OnError: func(err error, ctx telebot.Context) {
			log.Error("telegram error", "error", err)
		},
	}

	bot, err := telebot.NewBot(pref)
	if err != nil {
		log.Error("create bot", "error", err)
		os.Exit(1)
	}

	// Hermes AI analyzer.
	ai := hermes.NewAnalyzer(cfg.OpenAIKey, cfg.OpenAIBaseURL, cfg.AIModel, log)

	// Wire everything.
	tgBot := telegram.NewBot(bot, store, ai)
	_ = tgBot // tgBot registers handlers in NewBot; bot starts via Start()

	log.Info("bot starting", "env", cfg.BotEnv)

	// Graceful shutdown.
	go func() {
		bot.Start()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutting down...")
	bot.Stop()
}
