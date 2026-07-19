package telegram

import (
	"regexp"
	"strings"
)

var contractIDRe = regexp.MustCompile(`baza\.krugo\.tours/contracts/([A-Za-z0-9_-]+)`)

var templatePlaceholders = []string{
	"(ссылка на договор)",
	"Название: НовыйПоставщик",
	"Цена: значение",
}
// validateTemplate checks that the message is not an unfilled template
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
