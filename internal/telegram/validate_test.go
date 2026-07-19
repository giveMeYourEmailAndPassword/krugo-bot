package telegram

import "testing"

func TestValidateTemplate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{
			name: "unfilled template from button — reject",
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
  Цена: значение

Блоки #3, #4 — по необходимости. Ненужные строки и блоки удалите.`,
			wantErr: true,
		},
		{
			name: "filled #1 change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX
  Номер заявки был: 777777
  Номер заявки стал: 111222

Оставьте только поля, которые нужно изменить`,
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
			name: "partially filled — reject",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: текущий
  Стал: ANEX
  Номер заявки был: 777777
  Номер заявки стал: 111222`,
			wantErr: true,
		},
		{
			name: "valid with netto change — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Нетто договора: 80 → 95

Остальное — не меняется`,
			wantErr: false,
		},
		{
			name: "add supplier with real values — accept",
			text: `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #2: добавить
  Название: KOMPAS
  Номер заявки: 222222
  Цена: 45`,
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
