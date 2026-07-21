package commands

import "errors"

var (
	ErrUnknownAction      = errors.New("неизвестный тип заявки")
	ErrMissingContractID  = errors.New("не указан договор: нужна полная ссылка baza.krugo.tours/contracts/<id>")
	ErrMissingAmount      = errors.New("не указана сумма")
	ErrMissingCurrency    = errors.New("не указана валюта (USD, EUR или KGS)")
	ErrMissingDate        = errors.New("не указана дата (ГГГГ-ММ-ДД)")
	ErrMissingProvider    = errors.New("не указан поставщик")
	ErrMissingField       = errors.New("не указано поле для изменения финансов")
	ErrNoFinanceChanges     = errors.New("нет изменений финансов договора")
	ErrMultipleFinanceChanges = errors.New("одно изменение финансов за заявку — отправьте отдельным сообщением для каждого поля")
	ErrCancelContractUnsupported = errors.New("отмена договора недоступна через бот — используйте веб-интерфейс baza.krugo.tours (требуется согласие и файлы)")
	ErrMixedUnsupported = errors.New("нельзя смешивать поставщиков и финансы в одной заявке — отправьте отдельными сообщениями: одно для поставщиков, другое для финансов")
	ErrEmptyRawText       = errors.New("пустой текст заявки")
	ErrParsePaymentMethod = errors.New("не удалось распознать способ оплаты")
	ErrParseAmount        = errors.New("не удалось распознать сумму")
	ErrParseArrow         = errors.New("не удалось распознать изменение (формат: было → стало)")
)
