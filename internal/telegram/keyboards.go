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

Поставщик #1: точное имя → точное имя
  Номер заявки: старый → новый
  Сумма: старая → новая

Нетто: старое → новое
Брутто: старое → новое

Если поле менять не нужно — удалите всю строку перед отправкой.
Остальное — не меняется`
}
