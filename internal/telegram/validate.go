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

// placeholderRe matches any <...> marker used in contractTemplate().
// Catches unfilled template fields without maintaining a manual list.
var placeholderRe = regexp.MustCompile(`<[^>\n]+>`)
// validateTemplate checks that the message is not an unfilled template
func validateTemplate(text string) string {
	lower := strings.ToLower(text)

	for _, p := range templatePlaceholders {
		if strings.Contains(lower, strings.ToLower(p)) {
			return "заполните или удалите незаполненные строки шаблона"
		}
	}

	// Catch any <...> placeholder marker from contractTemplate().
	if placeholderRe.MatchString(text) {
		return "заполните или удалите незаполненные поля (<...>)"
	}

	if !contractIDRe.MatchString(text) {
		return "укажите полную ссылку на договор с ID"
	}

	return ""
}
