package pb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// newTestClient spins up an httptest server that mimics the PB endpoints
// the client uses (auth, CRUD, list) and returns a client pointed at it.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "admin@test.com", "test1234")
}

func TestAuthenticate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/_superusers/auth-with-password") {
			t.Fatalf("path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["identity"] != "admin@test.com" || body["password"] != "test1234" {
			t.Fatalf("creds: %+v", body)
		}
		json.NewEncoder(w).Encode(map[string]string{"token": "tok123"})
	})
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c.token != "tok123" {
		t.Fatalf("token: %q", c.token)
	}
}

func TestCreate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method: %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/payments/records") {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok123" {
			t.Fatalf("auth: %s", r.Header.Get("Authorization"))
		}
		var body Record
		json.NewDecoder(r.Body).Decode(&body)
		if body["amount"].(float64) != 50000 {
			t.Fatalf("amount: %v", body["amount"])
		}
		json.NewEncoder(w).Encode(Record{"id": "rec1", "amount": body["amount"]})
	})
	c.token = "tok123"
	rec, err := c.Create(context.Background(), "payments", Record{"amount": 50000.0, "currency": "KGS"})
	if err != nil {
		t.Fatal(err)
	}
	if rec["id"] != "rec1" {
		t.Fatalf("id: %v", rec["id"])
	}
}

func TestCreateReauthOn401(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/auth-with-password") {
			json.NewEncoder(w).Encode(map[string]string{"token": "newtok"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer newtok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(Record{"id": "ok"})
	})
	// First call has empty token → ensureAuth → auth → create.
	rec, err := c.Create(context.Background(), "payments", Record{"amount": 100})
	if err != nil {
		t.Fatal(err)
	}
	if rec["id"] != "ok" {
		t.Fatalf("id: %v", rec["id"])
	}
	if c.token != "newtok" {
		t.Fatalf("token after reauth: %q", c.token)
	}
}

func TestRetryOn401ExpiredToken(t *testing.T) {
	createCalls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth-with-password") {
			json.NewEncoder(w).Encode(map[string]string{"token": "freshtok"})
			return
		}
		// /payments/records — first attempt with stale token → 401.
		createCalls++
		if createCalls == 1 && r.Header.Get("Authorization") == "Bearer stale" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer freshtok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(Record{"id": "ok"})
	})
	c.token = "stale" // simulate expired token
	rec, err := c.Create(context.Background(), "payments", Record{"amount": 500})
	if err != nil {
		t.Fatal(err)
	}
	if rec["id"] != "ok" {
		t.Fatalf("id: %v", rec["id"])
	}
	if createCalls != 2 {
		t.Fatalf("create calls: %d want 2", createCalls)
	}
	if c.token != "freshtok" {
		t.Fatalf("token: %q want freshtok", c.token)
	}
}

func TestAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"message": "validation error",
			"data": map[string]any{
				"amount": map[string]string{"message": "amount must be > 0"},
			},
		})
	})
	c.token = "tok"
	_, err := c.Create(context.Background(), "payments", Record{"amount": -1})
	if err == nil {
		t.Fatal("expected error")
	}
	api, ok := err.(*APIError)
	if !ok {
		t.Fatalf("type: %T", err)
	}
	if api.Status != 400 {
		t.Fatalf("status: %d", api.Status)
	}
	if !strings.Contains(api.Body, "amount must be > 0") {
		t.Fatalf("body: %q", api.Body)
	}
}

func TestList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") != `contract_id="abc"` {
			t.Fatalf("filter: %q", r.URL.Query().Get("filter"))
		}
		json.NewEncoder(w).Encode(ListResult{
			Items: []Record{{"id": "p1"}, {"id": "p2"}},
			Total: 2,
		})
	})
	c.token = "tok"
	res, err := c.List(context.Background(), "payments", `contract_id="abc"`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 2 || len(res.Items) != 2 {
		t.Fatalf("total=%d items=%d", res.Total, len(res.Items))
	}
}

func TestResolveProviderID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ListResult{
			Items: []Record{
				{"id": "pid1", "name": "ANEX"},
				{"id": "pid2", "name": "BEST SERVICE"},
			},
			Total: 2,
		})
	})
	c.token = "tok"
	id, err := c.ResolveProviderID(context.Background(), "anex")
	if err != nil {
		t.Fatal(err)
	}
	if id != "pid1" {
		t.Fatalf("id: %q", id)
	}
	// Not found
	if _, err := c.ResolveProviderID(context.Background(), "UNKNOWN"); err != ErrNotFound {
		t.Fatalf("err: %v", err)
	}
}

func TestResolvePaymentMethodID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(ListResult{
			Items: []Record{
				{"id": "m1", "name": "Наличные", "short_name": "cash", "is_active": true},
				{"id": "m2", "name": "Банк", "short_name": "bank", "is_active": false},
			},
		})
	})
	c.token = "tok"
	id, err := c.ResolvePaymentMethodID(context.Background(), "наличные")
	if err != nil {
		t.Fatal(err)
	}
	if id != "m1" {
		t.Fatalf("id: %q", id)
	}
	// inactive ignored
	if _, err := c.ResolvePaymentMethodID(context.Background(), "bank"); err != ErrNotFound {
		t.Fatalf("err: %v", err)
	}
}

func TestFindPendingCorrection(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("filter"), `status="pending"`) {
			t.Fatalf("filter: %q", r.URL.Query().Get("filter"))
		}
		json.NewEncoder(w).Encode(ListResult{
			Items: []Record{{"id": "corr1", "status": "pending"}},
			Total: 1,
		})
	})
	c.token = "tok"
	rec, err := c.FindPendingCorrection(context.Background(), "app1")
	if err != nil {
		t.Fatal(err)
	}
	if rec["id"] != "corr1" {
		t.Fatalf("id: %v", rec["id"])
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(ErrNotFound) {
		t.Fatal("ErrNotFound should be not found")
	}
	if IsNotFound(&APIError{Status: 400}) {
		t.Fatal("400 is not found")
	}
	if !IsNotFound(&APIError{Status: 404}) {
		t.Fatal("404 should be not found")
	}
}

func TestConcurrent401SingleRefresh(t *testing.T) {
	var authCalls, createCalls atomic.Int32
	// barrier ensures all n goroutines reach the 401 path (stale token)
	// before the auth handler is allowed to return a fresh token.
	var staleHits sync.WaitGroup
	const n = 10
	staleHits.Add(n)
	allowAuth := make(chan struct{})

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/auth-with-password") {
			authCalls.Add(1)
			// Block auth until all goroutines have hit the 401 path,
			// so concurrent 401s pile up before the refresh resolves.
			<-allowAuth
			json.NewEncoder(w).Encode(map[string]string{"token": "freshtok"})
			return
		}
		createCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer freshtok" {
			staleHits.Done() // signal a stale attempt 401'd
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(Record{"id": "ok"})
	})
	c.token = "stale" // all goroutines 401 on first attempt

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Create(context.Background(), "payments", Record{"amount": 100})
			errs <- err
		}()
	}

	// Once all n stale attempts have 401'd, release the auth barrier.
	// (Give the goroutines time to start; the auth handler blocks until
	// this channel closes.)
	go func() {
		staleHits.Wait()
		close(allowAuth)
	}()

	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := authCalls.Load(); got != 1 {
		t.Fatalf("auth calls: %d, want 1 (single refresh)", got)
	}
	// All n first attempts 401 (counted). After refresh, some goroutines
	// retry and succeed; depending on timing the total create calls fall
	// in [n, 2n]: every goroutine hits ≥1, and at most one retry each.
	got := createCalls.Load()
	if got < int32(n) || got > int32(2*n) {
		t.Fatalf("create calls: %d, want in [%d, %d]", got, n, 2*n)
	}
}
