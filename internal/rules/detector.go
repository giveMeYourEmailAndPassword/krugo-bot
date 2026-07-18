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
	}

	score := 0
	for _, m := range markers {
		if strings.Contains(text, m) {
			score++
		}
	}

	return score >= 2
}
