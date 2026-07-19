package telegram

import "gopkg.in/telebot.v3"

// mainKeyboard — кнопки после обработки заявки.
func mainKeyboard() *telebot.ReplyMarkup {
	markup := &telebot.ReplyMarkup{}
	markup.Inline(
		markup.Row(
			markup.Data("📝 Изменить договор", "tpl:contract_change"),
		),
	)
	return markup
}

// contractTemplate возвращает шаблон заявки на изменение договора.
func contractTemplate() string {
	return `Заявка на изменение договора

Договор: (ссылка на договор)

Поставщик #1: текущий → новый
  Номер заявки: текущий → новый

Нетто: текущее → новое
Брутто: текущее → новое

Оставьте только поля, которые нужно изменить`
}
