package telegram

import "gopkg.in/telebot.v3"

// mainKeyboard builds the primary inline keyboard for request actions.
func mainKeyboard() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	markup.Inline(
		markup.Row(
			markup.Data("Передать в dev", "action:ready_for_dev"),
			markup.Data("Нужно уточнение", "action:needs_clarification"),
		),
		markup.Row(
			markup.Data("Назначить", "action:assigned"),
			markup.Data("Закрыть", "action:done"),
		),
	)
	return markup
}

// closeKeyboard builds a simple close-only inline keyboard.
func closeKeyboard() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	markup.Inline(
		markup.Row(
			markup.Data("Закрыть", "action:done"),
		),
	)
	return markup
}
