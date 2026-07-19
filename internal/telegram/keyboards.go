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

Поставщик #1: изменить
  Был: текущий
  Стал: новый
  Номер заявки был: текущий
  Номер заявки стал: новый
  Нетто: текущее → новое
  Брутто: текущее → новое

Поставщик #2: добавить
  Название: НовыйПоставщик
  Номер заявки: новый
  Нетто: значение
  Брутто: значение

Блоки #3, #4 — по необходимости. Ненужные строки и блоки удалите перед отправкой.`
}
