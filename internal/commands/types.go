// Package commands defines typed commands parsed from manager request messages.
//
// The parser converts structured Telegram messages into typed commands.
// The executor validates each command server-side and calls PocketBase
// via a thin REST client. Hermes is NOT involved in this path — parsing is
// deterministic, so prompt injection cannot bypass authorization.
//
// MVP scope: only "create pending" operations that a manager is allowed
// to perform directly. All approve/confirm/cancel operations stay in the
// web UI for a bookkeeper — the bot never executes them.
package commands

// Action enumerates the typed operations the bot can perform.
type Action string

const (
	// ActChangeContract is the legacy free-form contract edit dispatched
	// to Hermes. It is the only non-deterministic action.
	ActChangeContract Action = "change_contract"

	// ActCreatePayment registers a client payment as pending. A bookkeeper
	// confirms it in the web UI to capture the exchange-rate snapshot.
	ActCreatePayment Action = "create_payment"

	// ActCreateClientRefund creates a pending client refund request.
	ActCreateClientRefund Action = "create_client_refund"

	// ActCreateOperatorRequest creates a pending operator payment request
	// (a request to pay a supplier). A bookkeeper commits the actual
	// operator_payment via the atomic custom route.
	ActCreateOperatorRequest Action = "create_operator_request"

	// ActCreateAppCorrection creates a pending application correction
	// (amount change on an approved supplier application).
	ActCreateAppCorrection Action = "create_app_correction"

	// ActCancelApplication creates a pending cancellation request for an
	// approved supplier application.
	ActCancelApplication Action = "cancel_application"

	// ActCreateFinanceChange creates pending finance_change_requests for
	// brutto/netto/actual_netto changes on an approved contract.
	ActCreateFinanceChange Action = "create_finance_change"

	// ActCancelContractUnsupported is a sentinel action for cancellation
	// settlement requests. This operation is NOT supported by the bot MVP:
	// it requires the Hono /api/cancellation-settlements/create endpoint
	// (FormData + consent files + server-side canonical snapshot rebuild).
	// The executor returns a clear error directing the user to the web UI.
	// This sentinel exists solely to prevent "Заявка на отмену договора"
	// from being misclassified as ActCancelApplication.
	ActCancelContractUnsupported Action = "cancel_contract_unsupported"

	// ActMixedUnsupported is a sentinel for messages that mix provider
	// blocks and finance changes in one "Заявка на изменение договора".
	// This is ambiguous: sending it to Hermes could partially apply
	// provider edits while ignoring finance, and the bot cannot split the
	// message safely. The handler rejects it before any mutation and asks
	// the user to send separate messages.
	ActMixedUnsupported Action = "mixed_unsupported"
)

// Command is a parsed, typed request ready for execution.
//
// Only the fields relevant to the Action are populated; the rest stay
// zero. The executor switches on Action and reads exactly the fields
// documented for that action — never the whole struct.
type Command struct {
	Action Action `json:"action"`

	// ContractID is the PocketBase contract record id extracted from the
	// baza.krugo.tours link. Required for every action.
	ContractID string `json:"contract_id"`

	// Payment fields (ActCreatePayment).
	Amount         float64 `json:"amount,omitempty"`
	Currency       string  `json:"currency,omitempty"`
	PaymentMethod  string  `json:"payment_method,omitempty"`  // human label, resolved to id by executor
	PaymentDate    string  `json:"payment_date,omitempty"`    // YYYY-MM-DD
	Comment        string  `json:"comment,omitempty"`
	ChangeAmount   float64 `json:"change_amount,omitempty"`
	ChangeCurrency string  `json:"change_currency,omitempty"`

	// Refund fields (ActCreateClientRefund).
	RefundAmount float64 `json:"refund_amount,omitempty"`
	RefundReason string  `json:"refund_reason,omitempty"` // cancellation/overpayment/partial_refund/other
	RefundDate   string  `json:"refund_date,omitempty"`

	// Operator payment request fields (ActCreateOperatorRequest).
	ProviderName    string  `json:"provider_name,omitempty"`
	ApplicationNo   string  `json:"application_no,omitempty"` // application.number to identify the app
	OperatorAmount  float64 `json:"operator_amount,omitempty"`
	RequestType     string  `json:"request_type,omitempty"` // full_remaining | advance
	IsPrepayment    bool    `json:"is_prepayment,omitempty"`

	// Application correction fields (ActCreateAppCorrection, ActCancelApplication).
	ApplicationID  string  `json:"application_id,omitempty"` // PB record id, resolved by executor
	OldAmount      float64 `json:"old_amount,omitempty"`
	NewAmount      float64 `json:"new_amount,omitempty"`
	OldCurrency    string  `json:"old_currency,omitempty"`
	NewCurrency    string  `json:"new_currency,omitempty"`
	CorrectionReason string `json:"correction_reason,omitempty"`

	// Finance change fields (ActCreateFinanceChange).
	// FinanceChanges holds one entry per field to change.
	FinanceChanges []FinanceChange `json:"finance_changes,omitempty"`

	// ChangeContract preserves the original raw text for the Hermes path.
	RawText string `json:"raw_text,omitempty"`
}

// FinanceChange describes a single brutto/netto/actual_netto change request.
type FinanceChange struct {
	Field    string  `json:"field"`     // brutto_price | netto_price | actual_netto_price
	OldValue float64 `json:"old_value"`
	NewValue float64 `json:"new_value"`
	Currency string  `json:"currency"`
	Reason   string  `json:"reason,omitempty"`
}

// Validate performs structural validation of the command before execution.
// It checks that required fields for the action are present and non-zero.
// It does NOT enforce PocketBase-level invariants. Because the executor
// authenticates as a superuser, PB collection rules are NOT applied and
// several request hooks (financial_review_guard, operator direct-write
// guard) have an isSuperuserRequest() bypass, so PB cannot be relied on
// to reject bad input. Only SQL triggers (immutability) and model hooks
// (audit/logging/netto recompute) still run for superusers.
//
// MVP GAP — ownership: without a Telegram→PB user identity mapping
// (deferred to Stage 1), the executor cannot check that the sender owns
// the target contract (created_by/created_by_2). Ownership is enforced
// only by the global Telegram allowlist (TELEGRAM_ALLOWED_USERS) — any
// allowlisted user may target any contract. The executor checks only
// what is available without identity: contract existence, finance_status,
// application status, currency match, provider existence, stale-change
// detection, and duplicate-pending detection.
func (c *Command) Validate() error {
	if c.ContractID == "" {
		return ErrMissingContractID
	}
	switch c.Action {
	case ActCreatePayment:
		if c.Amount <= 0 {
			return ErrMissingAmount
		}
		if c.Currency == "" {
			return ErrMissingCurrency
		}
	case ActCreateClientRefund:
		if c.RefundAmount <= 0 {
			return ErrMissingAmount
		}
		if c.Currency == "" {
			return ErrMissingCurrency
		}
		if c.RefundDate == "" {
			return ErrMissingDate
		}
	case ActCreateOperatorRequest:
		if c.OperatorAmount <= 0 {
			return ErrMissingAmount
		}
		if c.Currency == "" {
			return ErrMissingCurrency
		}
		if c.ProviderName == "" {
			return ErrMissingProvider
		}
	case ActCreateAppCorrection:
		if c.ApplicationID == "" && c.ApplicationNo == "" && c.ProviderName == "" {
			return ErrMissingProvider
		}
		if c.NewAmount <= 0 {
			return ErrMissingAmount
		}
	case ActCancelApplication:
		if c.ApplicationID == "" && c.ApplicationNo == "" && c.ProviderName == "" {
			return ErrMissingProvider
		}
	case ActCreateFinanceChange:
		// MVP: exactly one field per command to avoid partial state.
		if len(c.FinanceChanges) == 0 {
			return ErrNoFinanceChanges
		}
		if len(c.FinanceChanges) > 1 {
			return ErrMultipleFinanceChanges
		}
		fc := c.FinanceChanges[0]
		if fc.Field == "" {
			return ErrMissingField
		}
		if fc.NewValue <= 0 {
			return ErrMissingAmount
		}
	case ActChangeContract:
		// Hermes path — no structural validation beyond contract id.
		if c.RawText == "" {
			return ErrEmptyRawText
		}
	case ActCancelContractUnsupported:
		// Always valid structurally (just needs contract id, already checked).
		// The executor rejects it with a clear "use web UI" message.
	case ActMixedUnsupported:
		// Always valid structurally — the handler rejects before mutation.
	default:
		return ErrUnknownAction
	}
	return nil
}
