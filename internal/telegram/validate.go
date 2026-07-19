package telegram

import (
	"regexp"
	"strings"
)

var contractIDRe = regexp.MustCompile(`baza\.krugo\.tours/contracts/([A-Za-z0-9_-]+)`)

var templatePlaceholders = []string{
	"(ссылка на договор)", "Был: текущий", "Название: НовыйПоставщик",
	"Цена: значение", "текущий →", "текущее →", "текущая →",
}

// validateTemplate checks that the message is not an unfilled template
// and that it contains a valid contract URL with ID.
// Returns "" if valid, or an error message if rejected.
func validateTemplate(text string) string {
	lower := strings.ToLower(text)

	for _, p := range templatePlaceholders {
		if strings.Contains(lower, strings.ToLower(p)) {
			return "заполните или удалите незаполненные строки шаблона"
		}
	}

	if !contractIDRe.MatchString(text) {
		return "укажите полную ссылку на договор с ID"
	}

	return ""
}
