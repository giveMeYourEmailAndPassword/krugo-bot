package telegram

import "testing"

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{
			name:    "untouched button template — reject",
			text:    contractTemplate(),
			wantErr: true,
		},
		{
			name: "filled change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX
  Номер заявки был: 777777
  Номер заявки стал: 111222`,
			wantErr: false,
		},
		{
			name: "missing contract ID — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX`,
			wantErr: true,
		},
		{
			name: "add with placeholder name — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #2: добавить
  Название: <НАЗВАНИЕ>
  Номер заявки: 222222
  Сумма: 45`,
			wantErr: true,
		},
		{
			name: "sum left as placeholder — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #2: добавить
  Название: KOMPAS
  Номер заявки: 222222
  Сумма: <СУММА>`,
			wantErr: true,
		},
		{
			name: "currency left as placeholder — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Платёж клиента:
  Сумма: 50000
  Валюта: <VAL>
  Способ: наличные
  Дата: 2026-07-21`,
			wantErr: true,
		},
		{
			name: "date left as placeholder — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Платёж клиента:
  Сумма: 50000
  Валюта: KGS
  Способ: наличные
  Дата: <ГГГГ-ММ-ДД>`,
			wantErr: true,
		},
		{
			name: "valid netto change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Нетто договора: 80 → 95
Валюта: USD
Причина: доплата`,
			wantErr: false,
		},
		{
			name: "valid payment — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Платёж клиента:
  Сумма: 50000
  Валюта: KGS
  Способ: наличные
  Дата: 2026-07-21
  Комментарий: аванс`,
			wantErr: false,
		},
		{
			name: "real user message with partial arrow — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/a1h4z2hrrgi6zuz

Поставщик #1: изменить
  Был: ВИЗА СОЦ
  Стал: JoinUP
  Номер заявки был: 123
  Номер заявки стал: 777
  Сумма: 500 → 800

Поставщик #2: добавить
  Название: Великолепный Век
  Номер заявки: 90900
  Сумма: 700`,
			wantErr: false,
		},
		{
			name: "actual netto change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Фактическое нетто: 75 → 90
Валюта: USD
Причина: перерасчёт`,
			wantErr: false,
		},
		{
			name: "reason left as placeholder — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Нетто договора: 80 → 95
Валюта: USD
Причина: <ПРИЧИНА>`,
			wantErr: true,
		},
		{
			name: "filled payment only — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/q9wynhz1pi4tpvh

Платёж клиента:
  Сумма: 500
  Валюта: USD
  Способ: наличные
  Дата: 2026-07-21
  Комментарий: тест`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTemplate(tt.text)
			gotErr := err != ""
			if gotErr != tt.wantErr {
				t.Errorf("validateTemplate() error = %q, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
