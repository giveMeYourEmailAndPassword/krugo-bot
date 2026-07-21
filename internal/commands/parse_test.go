package commands

import (
	"testing"
)

func TestParse_Payment(t *testing.T) {
	cmd := Parse(`Заявка на платёж

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Сумма: 50000
Валюта: KGS
Способ: наличные
Дата: 2026-07-21
Комментарий: аванс от клиента`)
	if cmd.Action != ActCreatePayment {
		t.Fatalf("action: got %q want %q", cmd.Action, ActCreatePayment)
	}
	if cmd.ContractID != "t85493bo3ky8ccs" {
		t.Fatalf("contractID: got %q", cmd.ContractID)
	}
	if cmd.Amount != 50000 {
		t.Fatalf("amount: got %v", cmd.Amount)
	}
	if cmd.Currency != "KGS" {
		t.Fatalf("currency: got %q", cmd.Currency)
	}
	if cmd.PaymentMethod != "наличные" {
		t.Fatalf("method: got %q", cmd.PaymentMethod)
	}
	if cmd.PaymentDate != "2026-07-21" {
		t.Fatalf("date: got %q", cmd.PaymentDate)
	}
	if cmd.Comment != "аванс от клиента" {
		t.Fatalf("comment: got %q", cmd.Comment)
	}
}

func TestParse_PaymentNoDateDefaultsToday(t *testing.T) {
	cmd := Parse(`Заявка на платеж

Договор: https://baza.krugo.tours/contracts/abc123

Сумма: 4500
Валюта: USD`)
	if cmd.PaymentDate == "" {
		t.Fatal("date should default to today")
	}
}

func TestParse_PaymentWithChange(t *testing.T) {
	cmd := Parse(`Заявка на платёж

Договор: https://baza.krugo.tours/contracts/abc123

Сумма: 50000
Валюта: KGS
Сдача: 5000
Валюта сдачи: KGS`)
	if cmd.ChangeAmount != 5000 {
		t.Fatalf("changeAmount: got %v", cmd.ChangeAmount)
	}
	if cmd.ChangeCurrency != "KGS" {
		t.Fatalf("changeCurrency: got %q", cmd.ChangeCurrency)
	}
}

func TestParse_ClientRefund(t *testing.T) {
	cmd := Parse(`Заявка на возврат

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Сумма: 30000
Валюта: KGS
Причина: отмена тура
Дата: 2026-07-21`)
	if cmd.Action != ActCreateClientRefund {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if cmd.RefundAmount != 30000 {
		t.Fatalf("amount: got %v", cmd.RefundAmount)
	}
	if cmd.RefundReason != "cancellation" {
		t.Fatalf("reason: got %q want cancellation", cmd.RefundReason)
	}
	if cmd.RefundDate != "2026-07-21" {
		t.Fatalf("date: got %q", cmd.RefundDate)
	}
}

func TestParse_ClientRefundReasonOverpayment(t *testing.T) {
	cmd := Parse(`Заявка на возврат

Договор: https://baza.krugo.tours/contracts/abc

Сумма: 100
Валюта: USD
Причина: переплата`)
	if cmd.RefundReason != "overpayment" {
		t.Fatalf("reason: got %q want overpayment", cmd.RefundReason)
	}
}

func TestParse_ClientRefundReasonPartial(t *testing.T) {
	cmd := Parse(`Заявка на возврат клиенту

Договор: https://baza.krugo.tours/contracts/abc

Сумма: 50
Валюта: EUR
Причина: частичный возврат`)
	if cmd.RefundReason != "partial_refund" {
		t.Fatalf("reason: got %q want partial_refund", cmd.RefundReason)
	}
}

func TestParse_OperatorRequest(t *testing.T) {
	cmd := Parse(`Заявка на оплату поставщику

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик: ANEX
Номер заявки: 111222
Сумма: 4500
Валюта: USD
Тип: полный остаток`)
	if cmd.Action != ActCreateOperatorRequest {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if cmd.ProviderName != "ANEX" {
		t.Fatalf("provider: got %q", cmd.ProviderName)
	}
	if cmd.ApplicationNo != "111222" {
		t.Fatalf("appNo: got %q", cmd.ApplicationNo)
	}
	if cmd.OperatorAmount != 4500 {
		t.Fatalf("amount: got %v", cmd.OperatorAmount)
	}
	if cmd.RequestType != "full_remaining" {
		t.Fatalf("requestType: got %q", cmd.RequestType)
	}
}

func TestParse_OperatorRequestAdvance(t *testing.T) {
	cmd := Parse(`Заявка на оплату поставщика

Договор: https://baza.krugo.tours/contracts/abc

Поставщик: KOMPAS
Номер заявки: 222333
Сумма: 2000
Валюта: USD
Тип: аванс`)
	if cmd.RequestType != "advance" {
		t.Fatalf("requestType: got %q want advance", cmd.RequestType)
	}
	if !cmd.IsPrepayment {
		t.Fatal("isPrepayment should be true for advance")
	}
}

func TestParse_AppCorrection(t *testing.T) {
	cmd := Parse(`Заявка на корректировку поставщика

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик: ANEX
Номер заявки: 111222
Сумма: 85 → 80
Причина: скидка от поставщика`)
	if cmd.Action != ActCreateAppCorrection {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if cmd.ProviderName != "ANEX" {
		t.Fatalf("provider: got %q", cmd.ProviderName)
	}
	if cmd.OldAmount != 85 || cmd.NewAmount != 80 {
		t.Fatalf("amounts: old=%v new=%v", cmd.OldAmount, cmd.NewAmount)
	}
	if cmd.CorrectionReason != "скидка от поставщика" {
		t.Fatalf("reason: got %q", cmd.CorrectionReason)
	}
}

func TestParse_AppCorrectionWasBecameLines(t *testing.T) {
	cmd := Parse(`Заявка на корректировку

Договор: https://baza.krugo.tours/contracts/abc

Поставщик: ANEX
Номер заявки: 111222
Сумма была: 85
Сумма стала: 80`)
	if cmd.OldAmount != 85 || cmd.NewAmount != 80 {
		t.Fatalf("amounts: old=%v new=%v", cmd.OldAmount, cmd.NewAmount)
	}
}

func TestParse_CancelApplication(t *testing.T) {
	cmd := Parse(`Заявка на отмену поставщика

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик: KOMPAS
Номер заявки: 222222
Причина: отказ от услуги`)
	if cmd.Action != ActCancelApplication {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if cmd.ProviderName != "KOMPAS" {
		t.Fatalf("provider: got %q", cmd.ProviderName)
	}
	if cmd.CorrectionReason != "отказ от услуги" {
		t.Fatalf("reason: got %q", cmd.CorrectionReason)
	}
}

func TestParse_CancelContractNotMisclassifiedAsCancelApplication(t *testing.T) {
	cmd := Parse(`Заявка на отмену договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Возврат клиенту: 70000 KGS
Причина: отказ клиента от тура`)
	if cmd.Action != ActCancelContractUnsupported {
		t.Fatalf("action: got %q want %q (must NOT be cancel_application)", cmd.Action, ActCancelContractUnsupported)
	}
}

func TestParse_CancelContractVariantAnnulirovanie(t *testing.T) {
	cmd := Parse(`Заявка на аннулирование договора

Договор: https://baza.krugo.tours/contracts/abc`)
	if cmd.Action != ActCancelContractUnsupported {
		t.Fatalf("action: got %q", cmd.Action)
	}
}

func TestValidate_FinanceChangeRejectsMultiple(t *testing.T) {
	cmd := Command{
		Action: ActCreateFinanceChange,
		ContractID: "abc",
		FinanceChanges: []FinanceChange{
			{Field: "brutto_price", NewValue: 120},
			{Field: "netto_price", NewValue: 95},
		},
	}
	if err := cmd.Validate(); err != ErrMultipleFinanceChanges {
		t.Fatalf("err: %v want ErrMultipleFinanceChanges", err)
	}
}

func TestValidate_CancelContractUnsupported(t *testing.T) {
	cmd := Command{Action: ActCancelContractUnsupported, ContractID: "abc"}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestParse_FinanceChange(t *testing.T) {
	cmd := Parse(`Заявка на изменение финансов

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Брутто: 100 → 120
Нетто: 80 → 95
Валюта: USD
Причина: доплата за доп. услуги`)
	if cmd.Action != ActCreateFinanceChange {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if len(cmd.FinanceChanges) != 2 {
		t.Fatalf("changes: got %d want 2", len(cmd.FinanceChanges))
	}
	if cmd.FinanceChanges[0].Field != "brutto_price" || cmd.FinanceChanges[0].OldValue != 100 || cmd.FinanceChanges[0].NewValue != 120 {
		t.Fatalf("brutto: %+v", cmd.FinanceChanges[0])
	}
	if cmd.FinanceChanges[1].Field != "netto_price" || cmd.FinanceChanges[1].OldValue != 80 || cmd.FinanceChanges[1].NewValue != 95 {
		t.Fatalf("netto: %+v", cmd.FinanceChanges[1])
	}
	if cmd.Currency != "USD" {
		t.Fatalf("currency: got %q", cmd.Currency)
	}
}

func TestParse_FinanceChangeActualNetto(t *testing.T) {
	cmd := Parse(`Заявка на изменение финансов

Договор: https://baza.krugo.tours/contracts/abc

Фактическое нетто: 80 → 75`)
	if len(cmd.FinanceChanges) != 1 {
		t.Fatalf("changes: got %d want 1", len(cmd.FinanceChanges))
	}
	if cmd.FinanceChanges[0].Field != "actual_netto_price" {
		t.Fatalf("field: got %q", cmd.FinanceChanges[0].Field)
	}
}

func TestParse_LegacyChangeContract(t *testing.T) {
	raw := `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX`
	cmd := Parse(raw)
	if cmd.Action != ActChangeContract {
		t.Fatalf("action: got %q", cmd.Action)
	}
	if cmd.ContractID != "t85493bo3ky8ccs" {
		t.Fatalf("contractID: got %q", cmd.ContractID)
	}
	if cmd.RawText != raw {
		t.Fatal("rawText should preserve original")
	}
}

func TestParse_LegacyChangeContractFinanceOnly(t *testing.T) {
	raw := `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Нетто договора: 80 → 95
Брутто договора: 100 → 120`
	cmd := Parse(raw)
	if cmd.Action != ActCreateFinanceChange {
		t.Fatalf("action: got %q want %q (finance-only legacy must route to pending gate)", cmd.Action, ActCreateFinanceChange)
	}
	if len(cmd.FinanceChanges) != 2 {
		t.Fatalf("changes: got %d want 2", len(cmd.FinanceChanges))
	}
}

func TestParse_LegacyChangeContractProviderOnly(t *testing.T) {
	raw := `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX`
	cmd := Parse(raw)
	if cmd.Action != ActChangeContract {
		t.Fatalf("action: got %q want %q (provider-only stays legacy)", cmd.Action, ActChangeContract)
	}
}

func TestParse_LegacyChangeContractMixedRejected(t *testing.T) {
	raw := `Заявка на изменение договора

Договор: https://baza.krugo.tours/contracts/t85493bo3ky8ccs

Поставщик #1: изменить
  Был: BEST SERVICE
  Стал: ANEX

Нетто договора: 80 → 95`
	cmd := Parse(raw)
	if cmd.Action != ActMixedUnsupported {
		t.Fatalf("action: got %q want %q (mixed must be rejected, not sent to Hermes)", cmd.Action, ActMixedUnsupported)
	}
}

func TestParse_UnknownFallsBackToLegacy(t *testing.T) {
	cmd := Parse("какой-то текст без заголовка")
	if cmd.Action != ActChangeContract {
		t.Fatalf("action: got %q want %q", cmd.Action, ActChangeContract)
	}
}

func TestParse_Empty(t *testing.T) {
	cmd := Parse("")
	if cmd.Action != ActChangeContract {
		t.Fatalf("action: got %q", cmd.Action)
	}
}

func TestParse_MissingContractID(t *testing.T) {
	cmd := Parse("Заявка на платёж\n\nСумма: 500\nВалюта: USD")
	if cmd.ContractID != "" {
		t.Fatalf("contractID should be empty, got %q", cmd.ContractID)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     Command
		wantErr bool
	}{
		{
			name:    "payment ok",
			cmd:     Command{Action: ActCreatePayment, ContractID: "abc", Amount: 500, Currency: "USD"},
			wantErr: false,
		},
		{
			name:    "payment no contract",
			cmd:     Command{Action: ActCreatePayment, Amount: 500, Currency: "USD"},
			wantErr: true,
		},
		{
			name:    "payment no amount",
			cmd:     Command{Action: ActCreatePayment, ContractID: "abc", Currency: "USD"},
			wantErr: true,
		},
		{
			name:    "refund ok",
			cmd:     Command{Action: ActCreateClientRefund, ContractID: "abc", RefundAmount: 300, Currency: "KGS", RefundDate: "2026-07-21"},
			wantErr: false,
		},
		{
			name:    "refund no date",
			cmd:     Command{Action: ActCreateClientRefund, ContractID: "abc", RefundAmount: 300, Currency: "KGS"},
			wantErr: true,
		},
		{
			name:    "operator ok",
			cmd:     Command{Action: ActCreateOperatorRequest, ContractID: "abc", ProviderName: "ANEX", OperatorAmount: 4500, Currency: "USD"},
			wantErr: false,
		},
		{
			name:    "operator no provider",
			cmd:     Command{Action: ActCreateOperatorRequest, ContractID: "abc", OperatorAmount: 4500, Currency: "USD"},
			wantErr: true,
		},
		{
			name:    "correction ok",
			cmd:     Command{Action: ActCreateAppCorrection, ContractID: "abc", ProviderName: "ANEX", NewAmount: 80},
			wantErr: false,
		},
		{
			name:    "correction no amount",
			cmd:     Command{Action: ActCreateAppCorrection, ContractID: "abc", ProviderName: "ANEX"},
			wantErr: true,
		},
		{
			name:    "cancel app ok",
			cmd:     Command{Action: ActCancelApplication, ContractID: "abc", ProviderName: "KOMPAS"},
			wantErr: false,
		},
		{
			name:    "finance change ok",
			cmd:     Command{Action: ActCreateFinanceChange, ContractID: "abc", FinanceChanges: []FinanceChange{{Field: "brutto_price", NewValue: 120}}},
			wantErr: false,
		},
		{
			name:    "finance change empty",
			cmd:     Command{Action: ActCreateFinanceChange, ContractID: "abc"},
			wantErr: true,
		},
		{
			name:    "legacy ok",
			cmd:     Command{Action: ActChangeContract, ContractID: "abc", RawText: "text"},
			wantErr: false,
		},
		{
			name:    "legacy no raw",
			cmd:     Command{Action: ActChangeContract, ContractID: "abc"},
			wantErr: true,
		},
		{
			name:    "unknown action",
			cmd:     Command{Action: "bogus", ContractID: "abc"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cmd.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestAuthorTag(t *testing.T) {
	if got := AuthorTag(123, "john"); got != "@john (tg:123)" {
		t.Fatalf("got %q", got)
	}
	if got := AuthorTag(456, ""); got != "tg:456" {
		t.Fatalf("got %q", got)
	}
}

func TestParse_AmountThousandsSeparator(t *testing.T) {
	cmd := Parse(`Заявка на платёж

Договор: https://baza.krugo.tours/contracts/abc

Сумма: 50 000
Валюта: KGS`)
	if cmd.Amount != 50000 {
		t.Fatalf("amount: got %v want 50000", cmd.Amount)
	}
}
