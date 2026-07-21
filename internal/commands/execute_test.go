package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amantur/krugo-bot/internal/pb"
)

// testEnv spins up a mock PB server + mock backend /rates and returns
// an executor wired to both. The PB handler inspects requests and
// records what was sent.
type testEnv struct {
	pbSrv      *httptest.Server
	ratesSrv   *httptest.Server
	exec       *Executor
	pbHandler  func(w http.ResponseWriter, r *http.Request)
	lastCreate map[string]map[string]any // last create payload per collection
	lastPatch  map[string]map[string]any // last patch payload (collection → body)
}
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{lastCreate: map[string]map[string]any{}, lastPatch: map[string]map[string]any{}}

	// Mock PB server.
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {}
	env.pbSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Auth.
		if strings.HasSuffix(r.URL.Path, "/auth-with-password") {
			json.NewEncoder(w).Encode(map[string]string{"token": "tok"})
			return
		}
		// Dispatch to per-test handler.
		env.pbHandler(w, r)
	}))
	t.Cleanup(env.pbSrv.Close)

	// Mock backend /rates (public).
	env.ratesSrv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rates" {
			json.NewEncoder(w).Encode(map[string]any{
				"usd": map[string]any{"buy": 87.3, "sell": 87.8},
				"eur": map[string]any{"buy": 99.7, "sell": 100.7},
				"ts":  1234567890,
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(env.ratesSrv.Close)

	pbClient := pb.NewClient(env.pbSrv.URL, "admin@test.com", "pass")
	env.exec = NewExecutor(pbClient, env.ratesSrv.URL)
	return env
}

// captureCreate records the last create payload for a collection and
// returns a created record with the given id.
func (env *testEnv) captureCreate(id string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		env.lastCreate[strings.Split(r.URL.Path, "/")[3]] = body
		json.NewEncoder(w).Encode(map[string]any{"id": id, "status": body["status"]})
	}
}

// contractResponse returns a contract record with given fields.
func contractResponse(extra map[string]any) map[string]any {
	rec := map[string]any{"id": "c1", "office": "off1"}
	for k, v := range extra {
		rec[k] = v
	}
	return rec
}

func writeJSON(w http.ResponseWriter, v any) {
	json.NewEncoder(w).Encode(v)
}

// --- A1: client payment ---

func TestExec_CreatePaymentKGS(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/payments/records"):
			h := env.captureCreate("pay1")
			h(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreatePayment, ContractID: "c1", Amount: 50000, Currency: "KGS", PaymentDate: "2026-07-21"}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "john")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "pay1" {
		t.Fatalf("id: %v", res.RecordID)
	}
	payload := env.lastCreate["payments"]
	if payload["status"] != "pending" {
		t.Fatalf("status: %v", payload["status"])
	}
	if payload["is_confirmed"] != false {
		t.Fatalf("is_confirmed: %v", payload["is_confirmed"])
	}
	// KGS rate = 1.
	if v, ok := payload["exchange_rate_kgs"].(float64); !ok || v != 1 {
		t.Fatalf("rate: %v", payload["exchange_rate_kgs"])
	}
	// Author tag in comment.
	comment, _ := payload["comment"].(string)
	if !strings.Contains(comment, "@john (tg:123)") {
		t.Fatalf("comment: %q", comment)
	}
	// office from contract.
	if payload["office_id"] != "off1" {
		t.Fatalf("office: %v", payload["office_id"])
	}
}

func TestExec_CreatePaymentUSDWithRate(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/payments/records"):
			h := env.captureCreate("pay2")
			h(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreatePayment, ContractID: "c1", Amount: 4500, Currency: "USD"}
	res, err := env.exec.Execute(context.Background(), cmd, 999, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "pay2" {
		t.Fatalf("id: %v", res.RecordID)
	}
	// USD rate = 87.8 (sell).
	if v, ok := env.lastCreate["payments"]["exchange_rate_kgs"].(float64); !ok || v != 87.8 {
		t.Fatalf("rate: %v", env.lastCreate["payments"]["exchange_rate_kgs"])
	}
}

func TestExec_CreatePaymentCancelledContract(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, contractResponse(map[string]any{"is_cancelled": true}))
	}
	cmd := Command{Action: ActCreatePayment, ContractID: "c1", Amount: 50000, Currency: "KGS"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "john")
	if err == nil || !strings.Contains(err.Error(), "отменён") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_CreatePaymentEURRate(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/payments/records"):
			env.captureCreate("pay3")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreatePayment, ContractID: "c1", Amount: 3000, Currency: "EUR"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "john")
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := env.lastCreate["payments"]["exchange_rate_kgs"].(float64); !ok || v != 100.7 {
		t.Fatalf("rate: %v", env.lastCreate["payments"]["exchange_rate_kgs"])
	}
}

// --- A4: client refund ---

func TestExec_CreateClientRefund(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/client_refunds/records"):
			env.captureCreate("ref1")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateClientRefund, ContractID: "c1", RefundAmount: 30000, Currency: "KGS", RefundDate: "2026-07-21", RefundReason: "cancellation"}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "ref1" {
		t.Fatalf("id: %v", res.RecordID)
	}
	p := env.lastCreate["client_refunds"]
	if p["status"] != "pending" {
		t.Fatalf("status: %v", p["status"])
	}
	if p["reason"] != "cancellation" {
		t.Fatalf("reason: %v", p["reason"])
	}
	if c, _ := p["comment"].(string); !strings.Contains(c, "@jane") {
		t.Fatalf("comment: %q", c)
	}
}

// --- A5: operator request — author comment + hook error passthrough ---

func TestExec_CreateOperatorRequestAuthorComment(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1", "is_primary": true}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/operator_payment_requests/records") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"items": []any{}, "totalItems": 0})
		case strings.HasSuffix(r.URL.Path, "/operator_payment_requests/records") && r.Method == http.MethodPost:
			env.captureCreate("opr1")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateOperatorRequest, ContractID: "c1", ProviderName: "ANEX", OperatorAmount: 4500, Currency: "USD", RequestType: "full_remaining"}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "opr1" {
		t.Fatalf("id: %v", res.RecordID)
	}
	p := env.lastCreate["operator_payment_requests"]
	if c, _ := p["comment"].(string); !strings.Contains(c, "@bob (tg:123)") {
		t.Fatalf("comment: %q", c)
	}
	if p["status"] != "pending" {
		t.Fatalf("status: %v", p["status"])
	}
}

func TestExec_CreateOperatorRequestHookError(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/operator_payment_requests/records") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"items": []any{}, "totalItems": 0})
		case strings.HasSuffix(r.URL.Path, "/operator_payment_requests/records") && r.Method == http.MethodPost:
			// Simulate PB hook rejection (e.g. client not 100% paid).
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]any{"message": "Клиент не оплатил 100%"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateOperatorRequest, ContractID: "c1", ProviderName: "ANEX", OperatorAmount: 4500, Currency: "USD"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "bob")
	if err == nil || !strings.Contains(err.Error(), "Клиент не оплатил") {
		t.Fatalf("err: %v", err)
	}
}

// --- A6: correction create + upsert + stale ---

func TestExec_CreateAppCorrectionCreate(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			writeJSON(w, map[string]any{"id": "app1", "amount": 85.0, "currency": "USD"})
		case strings.Contains(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodGet:
			// No existing pending.
			writeJSON(w, map[string]any{"items": []any{}, "totalItems": 0})
		case strings.HasSuffix(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodPost:
			env.captureCreate("corr1")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 80, OldAmount: 85, Currency: "USD"}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "corr1" {
		t.Fatalf("id: %v", res.RecordID)
	}
	p := env.lastCreate["application_corrections"]
	if p["type"] != "correction" {
		t.Fatalf("type: %v", p["type"])
	}
	if p["old_amount"] != 85.0 || p["new_amount"] != 80.0 {
		t.Fatalf("amounts: old=%v new=%v", p["old_amount"], p["new_amount"])
	}
}

func TestExec_CreateAppCorrectionUpsertExisting(t *testing.T) {
	env := newTestEnv(t)
	updated := false
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			writeJSON(w, map[string]any{"id": "app1", "amount": 85.0, "currency": "USD"})
		case strings.Contains(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodGet:
			// Existing pending correction.
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "corr0", "status": "pending"}}, "totalItems": 1})
		case strings.Contains(r.URL.Path, "/application_corrections/records/corr0") && r.Method == http.MethodPatch:
			updated = true
			writeJSON(w, map[string]any{"id": "corr0", "status": "pending"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 78}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != nil {
		t.Fatal(err)
	}
	if !updated {
		t.Fatal("should upsert existing pending correction")
	}
	if res.RecordID != "corr0" {
		t.Fatalf("id: %v", res.RecordID)
	}
}

func TestExec_CreateAppCorrectionStaleReject(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			// Current amount is 90, but user said old=85 (stale).
			writeJSON(w, map[string]any{"id": "app1", "amount": 90.0, "currency": "USD"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 80, OldAmount: 85}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "уже изменилась") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_CreateAppCorrectionLookupErrorNoPost(t *testing.T) {
	createCalled := false
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			writeJSON(w, map[string]any{"id": "app1", "amount": 85.0, "currency": "USD"})
		case strings.Contains(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodGet:
			// Simulate PB lookup failure (500).
			w.WriteHeader(http.StatusInternalServerError)
		case strings.HasSuffix(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodPost:
			createCalled = true
			writeJSON(w, map[string]any{"id": "should-not-happen"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 80}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil {
		t.Fatal("expected lookup error")
	}
	if !strings.Contains(err.Error(), "поиск существующей корректировки") {
		t.Fatalf("err: %v", err)
	}
	if createCalled {
		t.Fatal("Create must NOT be called when lookup errors")
	}
}

// --- A7: cancellation ---

func TestExec_CancelApplication(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "KOMPAS"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			writeJSON(w, map[string]any{"id": "app1", "amount": 45.0, "currency": "USD"})
		case strings.Contains(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodGet:
			writeJSON(w, map[string]any{"items": []any{}, "totalItems": 0})
		case strings.HasSuffix(r.URL.Path, "/application_corrections/records") && r.Method == http.MethodPost:
			env.captureCreate("corr2")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCancelApplication, ContractID: "c1", ProviderName: "KOMPAS"}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "corr2" {
		t.Fatalf("id: %v", res.RecordID)
	}
	p := env.lastCreate["application_corrections"]
	if p["type"] != "cancellation" {
		t.Fatalf("type: %v", p["type"])
	}
	// Cancellation: old_amount = current, new_amount = current (not 0).
	if p["old_amount"] != 45.0 || p["new_amount"] != 45.0 {
		t.Fatalf("amounts: old=%v new=%v", p["old_amount"], p["new_amount"])
	}
}

// --- A8: finance change ---

func TestExec_CreateFinanceChangeBrutto(t *testing.T) {
	env := newTestEnv(t)
	patchSeen := false
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodGet:
			writeJSON(w, contractResponse(map[string]any{"brutto_price": 100.0, "netto_price": 80.0}))
		case strings.HasSuffix(r.URL.Path, "/finance_change_requests/records") && r.Method == http.MethodPost:
			env.captureCreate("fcr1")(w, r)
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodPatch:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			env.lastPatch["contracts"] = body
			patchSeen = true
			writeJSON(w, contractResponse(map[string]any{"finance_status": "pending"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateFinanceChange, ContractID: "c1", Currency: "USD",
		FinanceChanges: []FinanceChange{{Field: "brutto_price", OldValue: 100, NewValue: 120}}}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != nil {
		t.Fatal(err)
	}
	if res.RecordID != "fcr1" {
		t.Fatalf("id: %v", res.RecordID)
	}
	p := env.lastCreate["finance_change_requests"]
	if p["field"] != "brutto_price" || p["old_value"] != 100.0 || p["new_value"] != 120.0 {
		t.Fatalf("field/vals: %+v", p)
	}
	if p["status"] != "pending" {
		t.Fatalf("status: %v", p["status"])
	}
	// Verify the contract PATCH set finance_status=pending.
	if !patchSeen {
		t.Fatal("contract PATCH not called")
	}
	if env.lastPatch["contracts"]["finance_status"] != "pending" {
		t.Fatalf("patch finance_status: %v", env.lastPatch["contracts"]["finance_status"])
	}
}

func TestExec_CreateFinanceChangeStaleReject(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodGet:
			// Current brutto is 110, user said old=100 (stale).
			writeJSON(w, contractResponse(map[string]any{"brutto_price": 110.0}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateFinanceChange, ContractID: "c1",
		FinanceChanges: []FinanceChange{{Field: "brutto_price", OldValue: 100, NewValue: 120}}}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "брутто уже изменилось") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_CreateFinanceChangeStatusUpdateFailure(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodGet:
			writeJSON(w, contractResponse(map[string]any{"brutto_price": 100.0}))
		case strings.HasSuffix(r.URL.Path, "/finance_change_requests/records") && r.Method == http.MethodPost:
			env.captureCreate("fcr2")(w, r)
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodPatch:
			// Status update fails.
			w.WriteHeader(http.StatusForbidden)
			writeJSON(w, map[string]any{"message": "forbidden"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateFinanceChange, ContractID: "c1",
		FinanceChanges: []FinanceChange{{Field: "brutto_price", NewValue: 120}}}
	res, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil {
		t.Fatal("expected error on status update failure")
	}
	// Request was created despite status update failure.
	if res.RecordID != "fcr2" {
		t.Fatalf("id: %v want fcr2", res.RecordID)
	}
	if !strings.Contains(err.Error(), "ошибка обновления статуса") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_CreateFinanceChangeMultipleRejected(t *testing.T) {
	env := newTestEnv(t)
	cmd := Command{Action: ActCreateFinanceChange, ContractID: "c1",
		FinanceChanges: []FinanceChange{
			{Field: "brutto_price", NewValue: 120},
			{Field: "netto_price", NewValue: 95},
		}}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "одно изменение") {
		t.Fatalf("err: %v", err)
	}
}

// --- A9: cancellation contract unsupported ---

func TestExec_CancelContractUnsupported(t *testing.T) {
	env := newTestEnv(t)
	cmd := Command{Action: ActCancelContractUnsupported, ContractID: "c1"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != ErrCancelContractUnsupported {
		t.Fatalf("err: %v want ErrCancelContractUnsupported", err)
	}
}

func TestExec_MixedUnsupported(t *testing.T) {
	env := newTestEnv(t)
	cmd := Command{Action: ActMixedUnsupported, ContractID: "c1"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err != ErrMixedUnsupported {
		t.Fatalf("err: %v want ErrMixedUnsupported", err)
	}
}

func TestExec_ApproxEqual(t *testing.T) {
	if !approxEqual(85.0, 85.0) {
		t.Fatal("exact equal")
	}
	if !approxEqual(85.0, 85.005) {
		t.Fatal("within tolerance")
	}
	if approxEqual(85.0, 85.5) {
		t.Fatal("outside tolerance")
	}
}

// --- validation gate tests ---

func TestExec_A6RejectsNonApprovedApp(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			// finance_status=pending — not approved.
			writeJSON(w, map[string]any{"id": "app1", "amount": 85.0, "currency": "USD", "finance_status": "pending"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 80}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "не подтверждена") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_A6RejectsDeletedApp(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1"):
			writeJSON(w, contractResponse(nil))
		case strings.HasSuffix(r.URL.Path, "/providers/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "pid1", "name": "ANEX"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records"):
			writeJSON(w, map[string]any{"items": []map[string]any{{"id": "app1"}}, "totalItems": 1})
		case strings.HasSuffix(r.URL.Path, "/applications/records/app1"):
			writeJSON(w, map[string]any{"id": "app1", "amount": 85.0, "currency": "USD", "is_deleted": true})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateAppCorrection, ContractID: "c1", ProviderName: "ANEX", NewAmount: 80}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "удалена") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_A8RejectsPendingContract(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/contracts/records/c1") && r.Method == http.MethodGet:
			writeJSON(w, contractResponse(map[string]any{"brutto_price": 100.0, "finance_status": "pending"}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
	cmd := Command{Action: ActCreateFinanceChange, ContractID: "c1",
		FinanceChanges: []FinanceChange{{Field: "brutto_price", NewValue: 120}}}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "на рассмотрении") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_A1RejectsDeletedContract(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, contractResponse(map[string]any{"is_deleted": true}))
	}
	cmd := Command{Action: ActCreatePayment, ContractID: "c1", Amount: 50000, Currency: "KGS"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "john")
	if err == nil || !strings.Contains(err.Error(), "удалён") {
		t.Fatalf("err: %v", err)
	}
}

func TestExec_A4RejectsRejectedContract(t *testing.T) {
	env := newTestEnv(t)
	env.pbHandler = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, contractResponse(map[string]any{"is_rejected": true}))
	}
	cmd := Command{Action: ActCreateClientRefund, ContractID: "c1", RefundAmount: 30000, Currency: "KGS", RefundDate: "2026-07-21"}
	_, err := env.exec.Execute(context.Background(), cmd, 123, "jane")
	if err == nil || !strings.Contains(err.Error(), "отклонён") {
		t.Fatalf("err: %v", err)
	}
}
