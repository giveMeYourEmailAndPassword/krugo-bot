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

Договор: https://baza.krugo.tours/contracts/__________

Поставщик #1: СТАРЫЙ → НОВЫЙ
  Номер заявки: СТАРЫЙ → НОВЫЙ
  Сумма: СТАРОЕ → НОВОЕ

Поставщик #2: добавить НАЗВАНИЕ
  Номер заявки: __________
  Сумма: __________

Нетто: СТАРОЕ → НОВОЕ
Брутто: СТАРОЕ → НОВОЕ

Остальное — не меняется`
}
