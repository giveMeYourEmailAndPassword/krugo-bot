package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gopkg.in/telebot.v3"
)

func main() {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		fmt.Fprintln(os.Stderr, "TELEGRAM_BOT_TOKEN is required")
		os.Exit(1)
	}

	pref := telebot.Settings{
		Token:  token,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
		OnError: func(err error, ctx telebot.Context) {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		},
	}

	bot, err := telebot.NewBot(pref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "create bot: %v\n", err)
		os.Exit(1)
	}

	bot.Handle("/start", func(c telebot.Context) error {
		return c.Send("Бот krugo-bot работает. Отправь заявку в группу — я её распознаю.")
	})

	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		text := c.Text()
		chat := c.Chat()
		fmt.Printf("[%s] %s: %s\n", chat.Title, c.Sender().Username, text)
		return c.Send("Сообщение получено.")
	})

	fmt.Println("Bot started. Press Ctrl+C to stop.")
	go bot.Start()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	fmt.Println("Shutting down...")
	bot.Stop()
}
