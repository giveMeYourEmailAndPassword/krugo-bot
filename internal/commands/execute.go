// Package commands executor: validates a typed Command against current
// PocketBase state and performs the create-pending write.
//
// SECURITY MODEL (read carefully):
//   - The executor authenticates as a PB superuser. Superuser requests
//     BYPASS collection rules and several request hooks (financial_review_guard,
//     operator direct-write guard) have an isSuperuserRequest() bypass.
//     Therefore PB does NOT reliably reject bad input for this client.
//   - The executor validates EVERY invariant itself in Go before writing:
//     contract existence, finance_status, application status, currency
//     match, provider existence, stale-change detection, duplicate-pending
//     detection. SQL triggers (immutability) and model hooks (audit/logging/
//     netto recompute) still run for superusers — those are fine.
//   - OWNERSHIP GAP (MVP): without a Telegram→PB user identity mapping
//     (deferred to Stage 1), the executor cannot check that the sender owns
//     the target contract (created_by/created_by_2). Ownership is enforced
//     only by the global Telegram allowlist — any allowlisted user may
//     target any contract. This is an accepted MVP limitation.
//   - The created_by relation is NOT filled (no real PB user id). The
//     author tag (Telegram username + id) is written into comment/reason
//     text fields so the bookkeeper can attribute the request.
//   - Only create-pending operations. No approve/confirm/commit — those
//     stay in the web UI for a bookkeeper (MVP scope).
package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/amantur/krugo-bot/internal/pb"
)

// Executor runs typed commands against PocketBase.
type Executor struct {
	pb        *pb.Client
	backendURL string
	http       *http.Client
}

// NewExecutor creates an executor. pbClient must be pre-configured with
// PB superuser credentials. backendURL is the contracts-backend (Hono)
// base URL for /api/rates (may be empty — rate-dependent ops will fail
// with a clear error).
func NewExecutor(pbClient *pb.Client, backendURL string) *Executor {
	return &Executor{
		pb:        pbClient,
		backendURL: strings.TrimRight(backendURL, "/"),
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// Result describes what the executor did.
type Result struct {
	Action      pbAction `json:"action"`
	Collection  string   `json:"collection"`
	RecordID    string   `json:"record_id,omitempty"`
	Status      string   `json:"status"`
	Summary     string   `json:"summary"`
}

// pbAction is a local alias to avoid importing commands types into json tags.
type pbAction = Action

// Execute validates and runs a command. Returns an error if validation
// fails or the PB write is rejected.
func (e *Executor) Execute(ctx context.Context, cmd Command, telegramID int64, username string) (Result, error) {
	if err := cmd.Validate(); err != nil {
		return Result{}, err
	}
	author := AuthorTag(telegramID, username)

	switch cmd.Action {
	case ActCreatePayment:
		return e.createPayment(ctx, cmd, author)
	case ActCreateClientRefund:
		return e.createClientRefund(ctx, cmd, author)
	case ActCreateOperatorRequest:
		return e.createOperatorRequest(ctx, cmd, author)
	case ActCreateAppCorrection:
		return e.createAppCorrection(ctx, cmd, author, false)
	case ActCancelApplication:
		return e.createAppCorrection(ctx, cmd, author, true)
	case ActCreateFinanceChange:
		return e.createFinanceChange(ctx, cmd, author)
	case ActCancelContractUnsupported:
		return Result{}, ErrCancelContractUnsupported
	case ActMixedUnsupported:
		return Result{}, ErrMixedUnsupported
	case ActChangeContract:
		return Result{}, fmt.Errorf("change_contract handled by Hermes, not executor")
	default:
		return Result{}, fmt.Errorf("неподдерживаемое действие: %s", cmd.Action)
	}
}

// --- helpers ---

// num extracts a float64 from a PB record field.
func num(r pb.Record, key string) float64 {
	switch v := r[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := parseFloat(v); err == nil {
			return f
		}
	}
	return 0
}

func parseFloat(s string) (float64, error) {
	s = strings.ReplaceAll(s, ",", ".")
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		return 0, err
	}
	return f, nil
}

// str extracts a string from a PB record field.
func str(r pb.Record, key string) string {
	if v, ok := r[key].(string); ok {
		return v
	}
	return ""
}

// floatPtr returns a pointer for a float64 (used to set optional fields).
func floatPtr(v float64) any { return v }

// --- A1: client payment ---

func (e *Executor) createPayment(ctx context.Context, cmd Command, author string) (Result, error) {
	// 1. Contract must exist and not be cancelled.
	contract, err := e.pb.GetContract(ctx, cmd.ContractID)
	if err != nil {
		return Result{}, fmt.Errorf("договор не найден: %w", err)
	}
	if err := validateActiveContract(contract); err != nil {
		return Result{}, err
	}

	// 2. Resolve payment_method_id (optional — if label given).
	var methodID string
	if cmd.PaymentMethod != "" {
		methodID, err = e.pb.ResolvePaymentMethodID(ctx, cmd.PaymentMethod)
		if err != nil {
			return Result{}, fmt.Errorf("способ оплаты не найден: %w", err)
		}
	}

	// 3. office_id from contract.
	officeID := str(contract, "office")

	// 4. exchange_rate_kgs: KGS=1 (or null), USD/EUR from /api/rates.
	var rateKGS any
	switch cmd.Currency {
	case "KGS":
		rateKGS = floatPtr(1)
	case "USD", "EUR":
		rate, err := e.fetchRate(ctx, cmd.Currency)
		if err != nil {
			return Result{}, fmt.Errorf("не удалось получить курс %s/KGS: %w", cmd.Currency, err)
		}
		rateKGS = floatPtr(rate)
	default:
		return Result{}, fmt.Errorf("неподдерживаемая валюта: %s", cmd.Currency)
	}

	// 5. Build payload mirroring frontend payment-manager onSubmit.
	payload := pb.Record{
		"comment":          defaultComment(cmd.Comment, "Платёж по договору"),
		"amount":           cmd.Amount,
		"currency":         cmd.Currency,
		"contract_id":      cmd.ContractID,
		"exchange_rate_kgs": rateKGS,
		"status":           "pending",
		"is_confirmed":     false,
		"payment_date":     cmd.PaymentDate,
	}
	if officeID != "" {
		payload["office_id"] = officeID
	}
	if methodID != "" {
		payload["payment_method_id"] = methodID
	}
	if cmd.ChangeAmount > 0 {
		payload["change_amount"] = cmd.ChangeAmount
		changeCur := cmd.ChangeCurrency
		if changeCur == "" {
			changeCur = cmd.Currency
		}
		payload["change_currency"] = changeCur
	}
	// Author attribution in comment (no created_by relation).
	payload["comment"] = fmt.Sprintf("%s | от: %s", payload["comment"], author)

	rec, err := e.pb.Create(ctx, "payments", payload)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Action:     ActCreatePayment,
		Collection:  "payments",
		RecordID:    str(rec, "id"),
		Status:      "pending",
		Summary:     fmt.Sprintf("Платёж %.0f %s создан, ожидает подтверждения бухгалтером", cmd.Amount, cmd.Currency),
	}, nil
}

// --- A4: client refund ---

func (e *Executor) createClientRefund(ctx context.Context, cmd Command, author string) (Result, error) {
	contract, err := e.pb.GetContract(ctx, cmd.ContractID)
	if err != nil {
		return Result{}, fmt.Errorf("договор не найден: %w", err)
	}
	if err := validateActiveContract(contract); err != nil {
		return Result{}, err
	}

	reason := cmd.RefundReason
	if reason == "" {
		reason = "other"
	}
	comment := cmd.Comment
	if comment == "" {
		comment = "Возврат клиенту"
	}
	comment = fmt.Sprintf("%s | от: %s", comment, author)

	payload := pb.Record{
		"contract_id": cmd.ContractID,
		"amount":      cmd.RefundAmount,
		"currency":    cmd.Currency,
		"refund_date": cmd.RefundDate,
		"reason":      reason,
		"comment":     comment,
		"status":      "pending",
	}

	rec, err := e.pb.Create(ctx, "client_refunds", payload)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Action:    ActCreateClientRefund,
		Collection: "client_refunds",
		RecordID:  str(rec, "id"),
		Status:    "pending",
		Summary:   fmt.Sprintf("Возврат %.0f %s создан, ожидает одобрения бухгалтером", cmd.RefundAmount, cmd.Currency),
	}, nil
}

// --- A5: operator payment request ---
//
// The PB hook operator_payment_requests.handleCreateRequest is the
// source of truth for business validation (finance status, application
// status, 100%-client-paid, currency match, remaining availability,
// duplicate detection, canonical snapshot). Superuser gets access bypass
// but NOT business-logic bypass — the hook still runs these checks. The
// executor does only light UX prechecks (contract existence, provider
// resolve) so bad references fail fast with a clear message; all
// financial eligibility is delegated to the PB create hook.

func (e *Executor) createOperatorRequest(ctx context.Context, cmd Command, author string) (Result, error) {
	// Light precheck: contract exists.
	if _, err := e.pb.GetContract(ctx, cmd.ContractID); err != nil {
		return Result{}, fmt.Errorf("договор не найден: %w", err)
	}

	// Resolve provider + application (needed to fill application_id).
	providerID, err := e.pb.ResolveProviderID(ctx, cmd.ProviderName)
	if err != nil {
		return Result{}, fmt.Errorf("поставщик не найден: %w", err)
	}
	appID, err := e.pb.ResolveApplicationID(ctx, cmd.ContractID, providerID, cmd.ApplicationNo)
	if err != nil {
		return Result{}, fmt.Errorf("заявка поставщика не найдена: %w", err)
	}

	// Light precheck: reject if an open request already exists (quick UX
	// fail; the PB hook also enforces this, but failing here avoids a
	// round-trip).
	existing, _ := e.pb.List(ctx, "operator_payment_requests",
		fmt.Sprintf(`application_id="%s" && (status="pending" || status="partially_paid")`, appID), 1)
	if existing.Total > 0 {
		return Result{}, fmt.Errorf("у заявки уже есть открытый запрос на оплату (статус: %s)", str(existing.Items[0], "status"))
	}

	comment := cmd.Comment
	if comment == "" {
		comment = "Запрос на оплату поставщику"
	}
	comment = fmt.Sprintf("%s | от: %s", comment, author)

	payload := pb.Record{
		"contract_id":      cmd.ContractID,
		"application_id":    appID,
		"request_type":     cmd.RequestType,
		"is_prepayment":     cmd.IsPrepayment,
		"requested_amount": cmd.OperatorAmount,
		"currency":         cmd.Currency,
		"status":           "pending",
		"comment":          comment,
	}

	rec, err := e.pb.Create(ctx, "operator_payment_requests", payload)
	if err != nil {
		// PB hook rejects with a business message — surface it directly.
		return Result{}, err
	}
	return Result{
		Action:     ActCreateOperatorRequest,
		Collection: "operator_payment_requests",
		RecordID:   str(rec, "id"),
		Status:     "pending",
		Summary:    fmt.Sprintf("Запрос на оплату %s %.0f %s создан, ожидает бухгалтера", cmd.ProviderName, cmd.OperatorAmount, cmd.Currency),
	}, nil
}

// --- A6/A7: application correction / cancellation ---

func (e *Executor) createAppCorrection(ctx context.Context, cmd Command, author string, cancel bool) (Result, error) {
	contract, err := e.pb.GetContract(ctx, cmd.ContractID)
	if err != nil {
		return Result{}, fmt.Errorf("договор не найден: %w", err)
	}
	if err := validateActiveContract(contract); err != nil {
		return Result{}, err
	}

	// Resolve provider + application.
	providerID, err := e.pb.ResolveProviderID(ctx, cmd.ProviderName)
	if err != nil {
		return Result{}, fmt.Errorf("поставщик не найден: %w", err)
	}
	appID, err := e.pb.ResolveApplicationID(ctx, cmd.ContractID, providerID, cmd.ApplicationNo)
	if err != nil {
		return Result{}, fmt.Errorf("заявка поставщика не найдена: %w", err)
	}

	app, err := e.pb.GetOne(ctx, "applications", appID)
	if err != nil {
		return Result{}, fmt.Errorf("заявка не загружена: %w", err)
	}

	// Application must be active and finance-approved. PB create hook
	// for application_corrections does NOT validate this for superuser,
	// and corrections/cancellations are meant for approved apps only —
	// non-approved apps should be edited directly (legacy path).
	if err := validateActiveApprovedApp(app); err != nil {
		return Result{}, err
	}

	// Stale-change detection: Telegram's old_value must match current PB.
	// For correction, old_amount must match app.amount; if not, reject
	// (the supplier price already changed — reject and ask for fresh).
	curAmount := num(app, "amount")
	curCur := str(app, "currency")

	correctionType := "correction"
	if cancel {
		correctionType = "cancellation"
	}

	reason := cmd.CorrectionReason
	if reason == "" {
		if cancel {
			reason = "Отмена поставщика"
		} else {
			reason = "Корректировка суммы"
		}
	}
	reason = fmt.Sprintf("%s | от: %s", reason, author)

	payload := pb.Record{
		"contract_id":    cmd.ContractID,
		"application_id": appID,
		"type":           correctionType,
		"field":          "amount",
		"status":         "pending",
		"reason":         reason,
	}

	if cancel {
		// Cancellation: old_amount from PB, new_amount = 0 is invalid in PB
		// (required number rejects 0) — frontend sets new_amount to current
		// and relies on type=cancellation. Mirror that.
		payload["old_amount"] = curAmount
		payload["new_amount"] = curAmount
		payload["old_currency"] = curCur
		payload["new_currency"] = curCur
	} else {
		// Correction: stale check.
		if cmd.OldAmount > 0 && !approxEqual(cmd.OldAmount, curAmount) {
			return Result{}, fmt.Errorf("сумма поставщика уже изменилась (текущая: %.2f %s). Создайте новую корректировку", curAmount, curCur)
		}
		payload["old_amount"] = curAmount
		payload["new_amount"] = cmd.NewAmount
		payload["old_currency"] = curCur
		newCur := cmd.NewCurrency
		if newCur == "" {
			newCur = curCur
		}
		payload["new_currency"] = newCur
	}

	// Upsert: if a pending correction exists, update it (mirror frontend
	// useCreateApplicationCorrection which upserts existing pending).
	// A lookup error (500/timeout) MUST NOT fall through to Create — that
	// would risk a duplicate pending correction. Only an explicit
	existing, err := e.pb.FindPendingCorrection(ctx, appID)
	if err != nil && !pb.IsNotFound(err) {
		return Result{}, fmt.Errorf("поиск существующей корректировки: %w", err)
	}
	if existing != nil {
		existID := str(existing, "id")
		if _, err := e.pb.Update(ctx, "application_corrections", existID, payload); err != nil {
			return Result{}, err
		}
		return Result{
			Action:     cmd.Action,
			Collection: "application_corrections",
			RecordID:   existID,
			Status:     "pending",
			Summary:    fmt.Sprintf("Существующая корректировка %s обновлена, ожидает бухгалтера", existID),
		}, nil
	}

	rec, err := e.pb.Create(ctx, "application_corrections", payload)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Action:    cmd.Action,
		Collection: "application_corrections",
		RecordID:  str(rec, "id"),
		Status:    "pending",
		Summary:   fmt.Sprintf("Корректировка поставщика %s создана, ожидает бухгалтера", cmd.ProviderName),
	}, nil
}

// --- A8: finance change request ---
//
// MVP: exactly ONE finance field per command (brutto OR netto OR
// actual_netto). Multiple fields would risk partial state — one Create
// succeeds, another fails, leaving the contract with a dangling request.
// The parser already splits "Брутто: A→B" and "Нетто: C→D" into separate
// FinanceChange entries; the executor rejects >1 to force the user to
// send separate messages per field.

func (e *Executor) createFinanceChange(ctx context.Context, cmd Command, author string) (Result, error) {
	if len(cmd.FinanceChanges) != 1 {
		return Result{}, fmt.Errorf("один запрос — одно изменение финансов. Отправьте отдельным сообщением для каждого поля")
	}
	fc := cmd.FinanceChanges[0]

	contract, err := e.pb.GetContract(ctx, cmd.ContractID)
	if err != nil {
		return Result{}, fmt.Errorf("договор не найден: %w", err)
	}
	if err := validateActiveContract(contract); err != nil {
		return Result{}, err
	}

	// Finance change requests are only created for approved contracts
	// (frontend: wasApproved = finance_status=="approved" || empty).
	// A pending/rejected contract means a request already exists or was
	// rejected — creating another via superuser would bypass the review
	// queue. "paid" is treated as approved-equivalent (runtime allows).
	fs := str(contract, "finance_status")
	if fs != "approved" && fs != "paid" && fs != "" {
		return Result{}, fmt.Errorf("финансы договора уже на рассмотрении или отклонены (статус: %s). Дождитесь решения бухгалтера", fs)
	}

	// Stale-change detection: OldValue must match current PB value.
	// If OldValue is 0 (user didn't specify "было"), we read it from PB.
	switch fc.Field {
	case "brutto_price":
		cur := num(contract, "brutto_price")
		if fc.OldValue > 0 && !approxEqual(fc.OldValue, cur) {
			return Result{}, fmt.Errorf("брутто уже изменилось (текущее: %.2f). Создайте новый запрос", cur)
		}
		fc.OldValue = cur
	case "netto_price":
		cur := num(contract, "netto_price")
		if fc.OldValue > 0 && !approxEqual(fc.OldValue, cur) {
			return Result{}, fmt.Errorf("нетто уже изменилось (текущее: %.2f). Создайте новый запрос", cur)
		}
		fc.OldValue = cur
	case "actual_netto_price":
		// actual_netto_price is stored as text; parse it.
		a := str(contract, "actual_netto_price")
		if a != "" {
			if parsed, err := parseFloat(a); err == nil {
				if fc.OldValue > 0 && !approxEqual(fc.OldValue, parsed) {
					return Result{}, fmt.Errorf("фактическое нетто уже изменилось (текущее: %s). Создайте новый запрос", a)
				}
				fc.OldValue = parsed
			}
		}
	default:
		return Result{}, fmt.Errorf("неподдерживаемое поле: %s", fc.Field)
	}

	reason := cmd.CorrectionReason
	if reason == "" {
		reason = "Изменение финансов"
	}
	reason = fmt.Sprintf("%s | от: %s", reason, author)

	payload := pb.Record{
		"contract_id": cmd.ContractID,
		"field":       fc.Field,
		"old_value":   fc.OldValue,
		"new_value":   fc.NewValue,
		"currency":    cmd.Currency,
		"status":      "pending",
		"reason":      reason,
	}
	rec, err := e.pb.Create(ctx, "finance_change_requests", payload)
	if err != nil {
		return Result{}, err
	}

	// Set contract finance_status to pending (mirror frontend
	// useUpdateContractMutation when wasApproved && hasFinanceChanges).
	// Error MUST be returned — leaving the request without the status
	// change would hide it from the bookkeeper inbox.
	if _, err := e.pb.Update(ctx, "contracts", cmd.ContractID, pb.Record{
		"finance_status": "pending",
	}); err != nil {
		return Result{
			Action:    ActCreateFinanceChange,
			Collection: "finance_change_requests",
			RecordID:  str(rec, "id"),
			Status:    "pending",
			Summary:   "Запрос создан, но не удалось перевести договор в ожидание бухгалтера",
		}, fmt.Errorf("запрос создан (%s), но ошибка обновления статуса договора: %w", str(rec, "id"), err)
	}

	return Result{
		Action:     ActCreateFinanceChange,
		Collection: "finance_change_requests",
		RecordID:   str(rec, "id"),
		Status:     "pending",
		Summary:    fmt.Sprintf("Запрос на изменение %s (%.2f → %.2f) создан, договор переведён в ожидание бухгалтера", fc.Field, fc.OldValue, fc.NewValue),
	}, nil
}

// --- rate fetcher (Hono backend public /rates) ---
//
// The Hono backend exposes a PUBLIC cached GET /rates (before the
// /api/* auth middleware) returning raw buy/sell rates:
//   {"usd": {buy, sell}, "eur": {buy, sell}, "ts": ...}
// PocketBase hooks use this same server-to-server fallback when they
// cannot forward an employee auth header. The executor calls it
// WITHOUT a bearer token — no superuser credentials are leaked.

type ratePair struct {
	Buy  float64 `json:"buy"`
	Sell float64 `json:"sell"`
}

type rateResp struct {
	USD ratePair `json:"usd"`
	EUR ratePair `json:"eur"`
}

// fetchRate returns the KGS sell rate for a currency (USD or EUR).
func (e *Executor) fetchRate(ctx context.Context, currency string) (float64, error) {
	if e.backendURL == "" {
		return 0, fmt.Errorf("BACKEND_URL не настроен")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.backendURL+"/rates", nil)
	if err != nil {
		return 0, err
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/rates: status %d", resp.StatusCode)
	}
	var rr rateResp
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return 0, fmt.Errorf("parse /rates: %w", err)
	}
	switch currency {
	case "USD":
		if rr.USD.Sell <= 0 {
			return 0, fmt.Errorf("курс USD не получен")
		}
		return rr.USD.Sell, nil
	case "EUR":
		if rr.EUR.Sell <= 0 {
			return 0, fmt.Errorf("курс EUR не получен")
		}
		return rr.EUR.Sell, nil
	}
	return 0, fmt.Errorf("нет курса для %s", currency)
}

// --- misc ---

// validateActiveContract rejects deleted, rejected, or cancelled
// contracts. Superuser bypasses PB rules, so the executor must check
// these flags itself — otherwise pending records could be created on
// dead contracts. Used by all actions that target a contract.
func validateActiveContract(contract pb.Record) error {
	if b, ok := contract["is_deleted"].(bool); ok && b {
		return fmt.Errorf("договор удалён")
	}
	if b, ok := contract["is_rejected"].(bool); ok && b {
		return fmt.Errorf("договор отклонён")
	}
	if b, ok := contract["is_cancelled"].(bool); ok && b {
		return fmt.Errorf("договор отменён")
	}
	return nil
}

// validateActiveApprovedApp rejects deleted, cancelled, or non-approved
// applications. Corrections and cancellations are meant for active,
// finance-approved supplier applications — non-approved apps can be
// edited directly (legacy path). The PB create hook for
// application_corrections does NOT enforce this for superuser, so the
// executor checks it explicitly. Empty finance_status is treated as
// approved (legacy compat, matching contracts hooks/frontend).
func validateActiveApprovedApp(app pb.Record) error {
	if b, ok := app["is_deleted"].(bool); ok && b {
		return fmt.Errorf("заявка поставщика удалена")
	}
	if s := str(app, "status"); s == "cancelled" || s == "refunded" {
		return fmt.Errorf("заявка поставщика %s — нельзя корректировать", s)
	}
	if fs := str(app, "finance_status"); fs != "approved" && fs != "" {
		return fmt.Errorf("заявка поставщика не подтверждена (статус: %s). Используйте редактирование договора", fs)
	}
	return nil
}

// approxEqual compares two money amounts with a 0.01 tolerance,
// matching the epsilon used by contracts PB hooks. Float equality
// (==/!=) would reject valid values due to binary representation.
func approxEqual(a, b float64) bool {
	if a == b {
		return true
	}
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= 0.01
}

func defaultComment(c, fallback string) string {
	if c == "" {
		return fallback
	}
	return c
}
