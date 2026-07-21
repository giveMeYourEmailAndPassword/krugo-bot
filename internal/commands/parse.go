package commands

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// contractIDRe extracts the PocketBase contract record id from a
// baza.krugo.tours link. The id is the last path segment.
var contractIDRe = regexp.MustCompile(`baza\.krugo\.tours/contracts/([A-Za-z0-9_-]+)`)

// amountRe matches a decimal number, optionally with thousands separators
// (spaces or no separators) — e.g. "50000", "4 500", "120.50".
var amountRe = regexp.MustCompile(`(\d[\d\s]*\.?\d*)`)

// arrowRe matches a "было → стало" pair used in templates.
var arrowRe = regexp.MustCompile(`([\d\s,.]+)\s*(?:→|->|=>)\s*([\d\s,.]+)`)

// Parse classifies a raw Telegram message into a typed Command.
//
// Classification is deterministic: the first line (or a known header)
// determines the action, then field regexes extract the payload. If the
// message matches none of the known request headers, it falls back to
// ActChangeContract (the legacy Hermes path) so existing behavior is
// preserved for free-form contract edit requests.
//
// Parse never returns an error for an unrecognized message — it returns a
// Command with Action=ActChangeContract and RawText set. Structural
// validation happens in Command.Validate, called by the executor before
// any PocketBase write.
func Parse(raw string) Command {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Command{Action: ActChangeContract, RawText: raw}
	}

	// Normalize the header line for classification: lowercase, trim.
	header := headerLine(raw)
	lower := strings.ToLower(header)

	contractID := extractContractID(raw)

	switch {
	// "Заявка на платёж" / "Заявка на платеж"
	// "Заявка на оплату поставщику" must precede "заявка на оплату" (HasPrefix collision).
	case startsWithAny(lower, "заявка на оплату поставщику", "заявка на оплату поставщика"):
		return parseOperatorRequest(raw, contractID)

	// "Заявка на платёж" / "Заявка на платеж" / "Заявка на оплату"
	case startsWithAny(lower, "заявка на платёж", "заявка на платеж", "заявка на оплату"):
		return parsePayment(raw, contractID)

	// "Заявка на возврат"
	case startsWithAny(lower, "заявка на возврат", "заявка на возврат клиенту"):
		return parseClientRefund(raw, contractID)


	// "Заявка на корректировку поставщика"
	case startsWithAny(lower, "заявка на корректировку поставщика", "заявка на корректировку"):
		return parseAppCorrection(raw, contractID, false)

	// "Заявка на отмену договора" — unsupported cancellation settlement.
	// Must precede "заявка на отмену поставщика" / "заявка на отмену"
	// (HasPrefix collision) so it is never misclassified as
	// ActCancelApplication. The executor returns a clear "use web UI" error.
	case startsWithAny(lower, "заявка на отмену договора", "заявка на аннулирование договора", "заявка на отмену контракта"):
		return Command{Action: ActCancelContractUnsupported, ContractID: contractID, RawText: raw}

	// "Заявка на отмену поставщика"
	case startsWithAny(lower, "заявка на отмену поставщика", "заявка на отмену"):
		return parseAppCorrection(raw, contractID, true)

	// "Заявка на изменение финансов" — note: must precede legacy "изменение договора".
	case startsWithAny(lower, "заявка на изменение финансов", "заявка на изменение фин", "заявка на финансы"):
		return parseFinanceChange(raw, contractID)

	// "Заявка на изменение договора" — legacy header. If it contains
	// finance lines ("Нетто/Брутто договора: A → B") WITHOUT provider
	// blocks ("Поставщик #N"), route to ActCreateFinanceChange so the
	// pending-request gate applies (Hermes must not directly PATCH
	// approved finances). Provider-only or mixed messages stay legacy.
	case startsWithAny(lower, "заявка на изменение договора"):
		return parseLegacyChangeContract(raw, contractID)

	// No known header → treat as legacy contract edit if it looks like a
	// request (the detector already gated this), else still legacy.
	default:
		return Command{Action: ActChangeContract, ContractID: contractID, RawText: raw}
	}
}

// --- helpers ---

func headerLine(raw string) string {
	for _, line := range strings.Split(raw, "\n") {
		l := strings.TrimSpace(line)
		if l != "" {
			return l
		}
	}
	return ""
}

func startsWithAny(s string, prefixes ...string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func extractContractID(raw string) string {
	m := contractIDRe.FindStringSubmatch(raw)
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// parseAmount extracts the first decimal number on the given line,
// stripping spaces used as thousands separators.
func parseAmount(line string) (float64, bool) {
	m := amountRe.FindStringSubmatch(line)
	if len(m) < 2 {
		return 0, false
	}
	s := strings.ReplaceAll(m[1], " ", "")
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseArrow extracts a (old, new) numeric pair from a line containing
// a → arrow.
func parseArrow(line string) (old, new float64, ok bool) {
	m := arrowRe.FindStringSubmatch(line)
	if len(m) < 3 {
		return 0, 0, false
	}
	o, err1 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(m[1]), " ", ""), 64)
	n, err2 := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(m[2]), " ", ""), 64)
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return o, n, true
}

// field returns the trimmed value after "label:" on a line, or "".
func field(lines []string, label string) string {
	lower := strings.ToLower(label)
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(strings.ToLower(t), lower+":") {
			return strings.TrimSpace(t[len(lower)+1:])
		}
	}
	return ""
}

// fieldAny returns the trimmed value after any of the given labels.
func fieldAny(lines []string, labels ...string) string {
	for _, label := range labels {
		if v := field(lines, label); v != "" {
			return v
		}
	}
	return ""
}

// today returns the current date as YYYY-MM-DD.
func today() string { return time.Now().Format("2006-01-02") }

// --- per-template parsers ---

func parsePayment(raw string, contractID string) Command {
	lines := strings.Split(raw, "\n")
	c := Command{Action: ActCreatePayment, ContractID: contractID}

	c.Amount, _ = parseAmount(fieldAny(lines, "сумма", "сумма платежа", "оплачено"))
	c.Currency = strings.ToUpper(fieldAny(lines, "валюта", "вал"))
	c.PaymentMethod = fieldAny(lines, "способ", "метод", "способ оплаты", "оплата")
	c.PaymentDate = fieldAny(lines, "дата", "дата платежа")
	if c.PaymentDate == "" {
		c.PaymentDate = today()
	}
	c.Comment = fieldAny(lines, "комментарий", "коммент", "примечание")

	// Change (сдача): optional "Сдача: 5000 KGS"
	if ch := fieldAny(lines, "сдача", "сдача платежа"); ch != "" {
		c.ChangeAmount, _ = parseAmount(ch)
	}
	c.ChangeCurrency = strings.ToUpper(fieldAny(lines, "валюта сдачи"))

	// Author note injected into comment by executor (see execute.go).
	return c
}

func parseClientRefund(raw string, contractID string) Command {
	lines := strings.Split(raw, "\n")
	c := Command{Action: ActCreateClientRefund, ContractID: contractID}

	c.RefundAmount, _ = parseAmount(fieldAny(lines, "сумма", "сумма возврата", "возврат"))
	c.Currency = strings.ToUpper(fieldAny(lines, "валюта", "вал"))
	c.RefundDate = fieldAny(lines, "дата", "дата возврата")
	if c.RefundDate == "" {
		c.RefundDate = today()
	}
	c.RefundReason = normalizeRefundReason(fieldAny(lines, "причина", "основание"))
	c.Comment = fieldAny(lines, "комментарий", "коммент", "примечание")
	return c
}

func normalizeRefundReason(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case strings.Contains(s, "отмен") || strings.Contains(s, "cancellation"):
		return "cancellation"
	case strings.Contains(s, "переплат") || strings.Contains(s, "overpayment"):
		return "overpayment"
	case strings.Contains(s, "частич") || strings.Contains(s, "partial"):
		return "partial_refund"
	default:
		return "other"
	}
}

func parseOperatorRequest(raw string, contractID string) Command {
	lines := strings.Split(raw, "\n")
	c := Command{Action: ActCreateOperatorRequest, ContractID: contractID}

	c.ProviderName = fieldAny(lines, "поставщик", "поставщик:", "наименование", "поставщику")
	c.ApplicationNo = fieldAny(lines, "номер заявки", "заявка поставщика", "номер")
	c.OperatorAmount, _ = parseAmount(fieldAny(lines, "сумма", "сумма оплаты", "сумма к оплате"))
	c.Currency = strings.ToUpper(fieldAny(lines, "валюта", "вал"))

	rt := strings.ToLower(fieldAny(lines, "тип", "тип оплаты", "вид"))
	switch {
	case strings.Contains(rt, "аванс") || strings.Contains(rt, "advance"):
		c.RequestType = "advance"
		c.IsPrepayment = true
	default:
		c.RequestType = "full_remaining"
	}
	c.Comment = fieldAny(lines, "комментарий", "коммент", "примечание")
	return c
}

func parseAppCorrection(raw string, contractID string, cancel bool) Command {
	lines := strings.Split(raw, "\n")
	if cancel {
		c := Command{Action: ActCancelApplication, ContractID: contractID}
		c.ProviderName = fieldAny(lines, "поставщик", "поставщик:", "наименование")
		c.ApplicationNo = fieldAny(lines, "номер заявки", "номер", "заявка поставщика")
		c.CorrectionReason = fieldAny(lines, "причина", "основание", "комментарий", "коммент")
		return c
	}

	c := Command{Action: ActCreateAppCorrection, ContractID: contractID}
	c.ProviderName = fieldAny(lines, "поставщик", "поставщик:", "наименование")
	c.ApplicationNo = fieldAny(lines, "номер заявки", "номер", "заявка поставщика")

	// Amount change: "Сумма: 85 → 80" or "Сумма была: 85 / стала: 80"
	if amountLine := fieldAny(lines, "сумма", "сумма поставщика", "цена"); amountLine != "" {
		if o, n, ok := parseArrow(amountLine); ok {
			c.OldAmount, c.NewAmount = o, n
		} else {
			// Single value = new amount
			c.NewAmount, _ = parseAmount(amountLine)
		}
	}
	// Was/became pair on separate lines
	if c.NewAmount == 0 {
		if was := fieldAny(lines, "сумма была", "была"); was != "" {
			c.OldAmount, _ = parseAmount(was)
		}
		if became := fieldAny(lines, "сумма стала", "стала"); became != "" {
			c.NewAmount, _ = parseAmount(became)
		}
	}
	c.OldCurrency = strings.ToUpper(fieldAny(lines, "валюта была", "валюта"))
	c.NewCurrency = strings.ToUpper(fieldAny(lines, "валюта стала", "валюта"))
	c.CorrectionReason = fieldAny(lines, "причина", "основание", "комментарий", "коммент")
	return c
}

func parseFinanceChange(raw string, contractID string) Command {
	lines := strings.Split(raw, "\n")
	c := Command{Action: ActCreateFinanceChange, ContractID: contractID}
	c.Currency = strings.ToUpper(fieldAny(lines, "валюта", "вал"))
	if c.Currency == "" {
		c.Currency = "USD"
	}

	// brutto first (no substring collisions), then actual_netto, then netto.
	// "фактическое нетто" contains "нетто" — exclude it for netto matching.
	c.FinanceChanges = append(c.FinanceChanges, parseFinanceLine(lines, "брутто", "brutto_price")...)
	c.FinanceChanges = append(c.FinanceChanges, parseFinanceLine(lines, "фактическое нетто", "actual_netto_price")...)
	c.FinanceChanges = append(c.FinanceChanges, parseFinanceLine(lines, "actual netto", "actual_netto_price")...)
	c.FinanceChanges = append(c.FinanceChanges, parseFinanceLineExcl(lines, "фактическое", "нетто", "netto_price")...)

	c.CorrectionReason = fieldAny(lines, "причина", "основание", "комментарий", "коммент")
	return c
}

// parseLegacyChangeContract handles "Заявка на изменение договора" — the
// old free-form header. It detects finance-only changes (Нетто/Брутто
// договора: A → B) without provider blocks and routes them to
// ActCreateFinanceChange so the pending-request gate applies (Hermes
// must not directly PATCH approved finances). Provider blocks or mixed
// content stays ActChangeContract (legacy Hermes path for editing
// suppliers — which is safe for non-approved applications).
func parseLegacyChangeContract(raw string, contractID string) Command {
	lines := strings.Split(raw, "\n")
	lower := strings.ToLower(raw)
	hasProvider := strings.Contains(lower, "поставщик #") || strings.Contains(lower, "поставщик:")

	// Detect finance lines with arrows (Брутто/Нетто договора: A → B).
	changes := []FinanceChange{}
	changes = append(changes, parseFinanceLine(lines, "брутто", "brutto_price")...)
	changes = append(changes, parseFinanceLine(lines, "фактическое нетто", "actual_netto_price")...)
	changes = append(changes, parseFinanceLine(lines, "actual netto", "actual_netto_price")...)
	changes = append(changes, parseFinanceLineExcl(lines, "фактическое", "нетто", "netto_price")...)

	hasFinance := len(changes) > 0

	if hasFinance && !hasProvider {
		// Finance-only legacy message → route to pending request gate.
		c := Command{Action: ActCreateFinanceChange, ContractID: contractID}
		c.Currency = strings.ToUpper(fieldAny(lines, "валюта", "вал"))
		if c.Currency == "" {
			c.Currency = "USD"
		}
		c.FinanceChanges = changes
		c.CorrectionReason = fieldAny(lines, "причина", "основание", "комментарий", "коммент")
		return c
	}
	if hasFinance && hasProvider {
		// Mixed: providers + finance in one message is ambiguous.
		// Sending to Hermes could partially apply provider edits while
		// ignoring finance. Reject before any mutation — the handler
		// returns a clear "split into separate messages" error.
		return Command{
			Action:     ActMixedUnsupported,
			ContractID: contractID,
			RawText:    raw,
		}
	}
	// Provider-only or no finance → legacy Hermes path.
	return Command{Action: ActChangeContract, ContractID: contractID, RawText: raw}
}

// parseFinanceLine extracts arrow-pair finance changes for a given label.
// Matches "Брутто: 100 → 120" or "Брутто договора: 100 → 120".
func parseFinanceLine(lines []string, label string, fieldName string) []FinanceChange {
	var out []FinanceChange
	for _, l := range lines {
		t := strings.TrimSpace(l)
		lt := strings.ToLower(t)
		if !strings.Contains(lt, strings.ToLower(label)) {
			continue
		}
		// Must contain an arrow with numbers
		if o, n, ok := parseArrow(t); ok {
			out = append(out, FinanceChange{
				Field:    fieldName,
				OldValue: o,
				NewValue: n,
			})
			return out
		}
	}
	return out
}

// parseFinanceLineExcl matches a label but skips lines containing the
// exclude token — e.g. "нетто" must not match "фактическое нетто".
func parseFinanceLineExcl(lines []string, exclude string, label string, fieldName string) []FinanceChange {
	var out []FinanceChange
	for _, l := range lines {
		t := strings.TrimSpace(l)
		lt := strings.ToLower(t)
		if strings.Contains(lt, strings.ToLower(exclude)) {
			continue
		}
		if !strings.Contains(lt, strings.ToLower(label)) {
			continue
		}
		if o, n, ok := parseArrow(t); ok {
			out = append(out, FinanceChange{Field: fieldName, OldValue: o, NewValue: n})
			return out
		}
	}
	return out
}


// AuthorTag builds the attribution string injected into text fields so the
// bookkeeper can see who filed the request via the bot. It is NOT placed
// in the created_by relation (which would need a real PB user id — Stage 1).
func AuthorTag(telegramID int64, username string) string {
	if username != "" {
		return fmt.Sprintf("@%s (tg:%d)", username, telegramID)
	}
	return fmt.Sprintf("tg:%d", telegramID)
}
