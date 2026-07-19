package telegram

import "testing"

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{
			name: "unfilled template — reject",
			text: `Заявка на изменение договора

Договор: (ссылка на договор)

Поставщик #1: изменить
  Был: текущий
  Стал: новый
  Номер заявки был: текущий
  Номер заявки стал: новый
  Цена: текущая → новая

Поставщик #2: добавить
  Название: НовыйПоставщик
  Номер заявки: новый
  Цена: значение`,
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
  Название: НовыйПоставщик
  Номер заявки: 222222
  Цена: 45`,
			wantErr: true,
		},
		{
			name: "price left as placeholder — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #2: добавить
  Название: KOMPAS
  Номер заявки: 222222
  Цена: значение`,
			wantErr: true,
		},
		{
			name: "valid netto change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Нетто договора: 80 → 95`,
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
  Цена: текущая → 800

Поставщик #2: добавить
  Название: Великолепный Век
  Номер заявки: 90900
  Цена: 700`,
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
