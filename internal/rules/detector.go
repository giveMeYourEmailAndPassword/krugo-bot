package rules

import "strings"

// LooksLikeRequest checks whether a message text matches the pattern
// of a client request template. Returns true when at least 2 markers match.
func LooksLikeRequest(text string) bool {
	text = strings.ToLower(text)

	markers := []string{
		"заявка",
		"клиент:",
		"проект:",
		"что изменить",
		"срочность",
		"дедлайн",
		"ошибка",
		"изменить",
		"добавить",
		"удалить",
		"договор",
		"поставщик",
		"нетто",
		"номер заявки",
		"baza.krugo.tours",
		"отель",
		"дата заезда",
		"дата выезда",
		"брутто",
		"клиент:",
		"сумма:",
		"доп.",
		"нетто договора",
		"брутто договора",
		// New request types (Stage 2 — create-pending operations).
		"заявка на платёж",
		"заявка на платеж",
		"заявка на оплату",
		"заявка на возврат",
		"заявка на корректировку",
		"заявка на отмену",
		"заявка на изменение финансов",
		"возврат клиенту",
		"оплата поставщику",
		"способ оплаты",
		"сдача",
		"причина:",
	}

	score := 0
	for _, m := range markers {
		if strings.Contains(text, m) {
			score++
		}
	}

	return score >= 2
}
