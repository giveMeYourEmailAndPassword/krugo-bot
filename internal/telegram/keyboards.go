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
Договор: https://baza.krugo.tours/contracts/

Поставщик #1: изменить
  Был:
  Стал:
  Номер заявки был:
  Номер заявки стал:
  Сумма была:
  Сумма стала:

Остальное не менять`
}
