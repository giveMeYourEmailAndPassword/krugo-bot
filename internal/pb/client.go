// Package pb is a thin REST client for PocketBase.
//
// It authenticates as a superuser (PB_USER/PB_PASS) and provides CRUD +
// lookup helpers. The executor uses it to resolve relation ids and to
// create pending records. The client NEVER executes approve/confirm/
// commit operations — those stay in the web UI for a bookkeeper (MVP).
//
// SECURITY: superuser requests BYPASS collection rules (listRule/
// createRule/updateRule/deleteRule). Several request hooks also have
// an explicit isSuperuserRequest() bypass — notably financial_review_guard
// and the operator_payments direct-write guard — so request hooks do NOT
// reliably reject bad input for superuser callers. SQL triggers
// (immutable_payments, settled_payments) and model hooks
// (onRecordAfterCreateSuccess) still run, but those do not enforce
// ownership, eligibility, currency, or status invariants for pending
// records. Therefore the executor (commands package) MUST validate every
// invariant itself before writing: contract finance_status, application
// status, currency match, provider existence, stale-change detection,
// and ownership are all checked in Go, not delegated to PB.
//
// The bot does NOT fill the created_by relation (it has no real PB user
// id without Stage 1 identity mapping); instead it writes an author tag
// into comment/reason text fields so the bookkeeper can attribute the
// request.
package pb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)
// Client talks to a PocketBase instance. It is safe for concurrent use:
// the token is guarded by mu (short get/set only), and re-authentication
// on 401 is serialized by refreshMu, which is held across the entire
// auth HTTP call so parallel 401s trigger only one refresh.
type Client struct {
	baseURL   string
	token     string
	mu        sync.Mutex // guards token get/set
	refreshMu sync.Mutex // serializes re-auth on 401
	http      *http.Client
	username  string
	password  string
}

// NewClient creates a PB client. Authenticate must be called before any
// data operation.
func NewClient(baseURL, user, pass string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
		username: user,
		password: pass,
	}
}

// Authenticate obtains a superuser token. The caller MUST NOT hold c.mu
// when calling this (it performs an HTTP request and then takes the lock
// only to store the token).
func (c *Client) Authenticate(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{
		"identity": c.username,
		"password": c.password,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/collections/_superusers/auth-with-password", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("pb auth: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pb auth: status %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("pb auth decode: %w", err)
	}
	c.mu.Lock()
	c.token = out.Token
	c.mu.Unlock()
	return nil
}

// currentToken returns the cached token under the lock.
func (c *Client) currentToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}


// ensureAuth re-authenticates if the token is empty. It routes through
// authenticateIfNeeded so cold-start auth is also serialized under
// refreshMu (no parallel Authenticate calls with the 401 path).
func (c *Client) ensureAuth(ctx context.Context) error {
	if c.currentToken() != "" {
		return nil
	}
	return c.authenticateIfNeeded(ctx, "")
}

// --- record helpers ---

// Record is a generic PB record (id + arbitrary fields).
type Record map[string]any

// requestBuilder builds an HTTP request bound to a fresh token. It is
// called once per attempt so that a retry after re-auth can re-create
// the request body (bytes.Reader is consumed after the first Do).
type requestBuilder func(token string) (*http.Request, error)

// do executes a request with a single 401-retry. On 401 it refreshes the
// token under refreshMu (held across the auth HTTP call so parallel 401s
// trigger only one refresh) and retries once. Any other error is returned
// as-is.
func (c *Client) do(ctx context.Context, build requestBuilder) (*http.Response, error) {
	if err := c.ensureAuth(ctx); err != nil {
		return nil, err
	}
	tok := c.currentToken()
	resp, err := c.attempt(ctx, build)
	if err != nil {
		return nil, err
	}
	// Retry once on 401 (expired/invalidated token).
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		if err := c.authenticateIfNeeded(ctx, tok); err != nil {
			return nil, err
		}
		return c.attempt(ctx, build)
	}
	return resp, nil
}

// authenticateIfNeeded is the single serialization point for all auth.
// refreshMu is held for the entire auth HTTP call so parallel callers
// (cold-start ensureAuth and 401-retry alike) trigger only one refresh.
//
// sentinel is the token value that means "needs refresh": "" for a
// cold-start empty token, or the specific failedToken for a 401 retry.
// After acquiring refreshMu, a double-check compares currentToken to
// sentinel: if they differ, another caller already refreshed — skip.
// The token is NOT cleared before auth (Authenticate overwrites it
// atomically under mu), so concurrent readers never see an empty token
// mid-refresh.
func (c *Client) authenticateIfNeeded(ctx context.Context, sentinel string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	if c.currentToken() != sentinel {
		// Another caller already refreshed (or token was never empty).
		return nil
	}
	return c.Authenticate(ctx)
}

// attempt builds the request with the current token and executes it.
func (c *Client) attempt(ctx context.Context, build requestBuilder) (*http.Response, error) {
	req, err := build(c.currentToken())
	if err != nil {
		return nil, err
	}
	return c.http.Do(req.WithContext(ctx))
}

// readError reads the response body and returns an APIError for status>=400.
func readError(collection string, resp *http.Response) error {
	b, _ := io.ReadAll(resp.Body)
	return &APIError{Collection: collection, Status: resp.StatusCode, Body: parsePBError(b)}
}

// Create inserts a record into a collection and returns the stored record.
func (c *Client) Create(ctx context.Context, collection string, payload Record) (Record, error) {
	body, _ := json.Marshal(payload)
	resp, err := c.do(ctx, func(token string) (*http.Request, error) {
		req, e := http.NewRequest(http.MethodPost, c.baseURL+"/api/collections/"+collection+"/records", bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("pb create %s: %w", collection, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, readError(collection, resp)
	}
	var rec Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, fmt.Errorf("pb create %s decode: %w", collection, err)
	}
	return rec, nil
}

// Update patches a record.
func (c *Client) Update(ctx context.Context, collection, id string, payload Record) (Record, error) {
	body, _ := json.Marshal(payload)
	resp, err := c.do(ctx, func(token string) (*http.Request, error) {
		req, e := http.NewRequest(http.MethodPatch, c.baseURL+"/api/collections/"+collection+"/records/"+id, bytes.NewReader(body))
		if e != nil {
			return nil, e
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("pb update %s/%s: %w", collection, id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, readError(collection, resp)
	}
	var rec Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, fmt.Errorf("pb update %s/%s decode: %w", collection, id, err)
	}
	return rec, nil
}

// GetOne fetches a single record by id.
func (c *Client) GetOne(ctx context.Context, collection, id string) (Record, error) {
	resp, err := c.do(ctx, func(token string) (*http.Request, error) {
		req, e := http.NewRequest(http.MethodGet, c.baseURL+"/api/collections/"+collection+"/records/"+id, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if err != nil {
		return nil, fmt.Errorf("pb get %s/%s: %w", collection, id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, readError(collection, resp)
	}
	var rec Record
	if err := json.NewDecoder(resp.Body).Decode(&rec); err != nil {
		return nil, fmt.Errorf("pb get %s/%s decode: %w", collection, id, err)
	}
	return rec, nil
}

// ListResult holds a page of records.
type ListResult struct {
	Items []Record `json:"items"`
	Total int      `json:"totalItems"`
}

// List fetches records matching a filter. filter should be a PB filter
// expression, e.g. `contract_id="abc" && status="pending"`.
func (c *Client) List(ctx context.Context, collection, filter string, perPage int) (ListResult, error) {
	params := url.Values{}
	if filter != "" {
		params.Set("filter", filter)
	}
	if perPage <= 0 {
		perPage = 30
	}
	params.Set("perPage", strconv.Itoa(perPage))
	reqURL := c.baseURL + "/api/collections/" + collection + "/records?" + params.Encode()
	resp, err := c.do(ctx, func(token string) (*http.Request, error) {
		req, e := http.NewRequest(http.MethodGet, reqURL, nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	})
	if err != nil {
		return ListResult{}, fmt.Errorf("pb list %s: %w", collection, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ListResult{}, readError(collection, resp)
	}
	var out ListResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ListResult{}, fmt.Errorf("pb list %s decode: %w", collection, err)
	}
	return out, nil
}

// --- lookup helpers used by the executor ---

// ResolveProviderID finds a provider record id by name (case-insensitive).
// Returns ErrNotFound when zero or more than one match.
func (c *Client) ResolveProviderID(ctx context.Context, name string) (string, error) {
	res, err := c.List(ctx, "providers", "", 200)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	var match string
	for _, item := range res.Items {
		n, _ := item["name"].(string)
		if strings.ToLower(n) == needle {
			if match != "" {
				return "", fmt.Errorf("несколько поставщиков с именем %q", name)
			}
			match = item["id"].(string)
		}
	}
	if match == "" {
		return "", ErrNotFound
	}
	return match, nil
}

// ResolvePaymentMethodID finds a payment_method by name (case-insensitive,
// also matches short_name). Returns ErrNotFound if no match.
func (c *Client) ResolvePaymentMethodID(ctx context.Context, name string) (string, error) {
	res, err := c.List(ctx, "payment_methods", "", 100)
	if err != nil {
		return "", err
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	var match string
	for _, item := range res.Items {
		n, _ := item["name"].(string)
		short, _ := item["short_name"].(string)
		active, _ := item["is_active"].(bool)
		if !active {
			continue
		}
		if strings.ToLower(n) == needle || strings.ToLower(short) == needle {
			if match != "" {
				return "", fmt.Errorf("несколько способов оплаты %q", name)
			}
			match = item["id"].(string)
		}
	}
	if match == "" {
		return "", ErrNotFound
	}
	return match, nil
}

// ResolveApplicationID finds an application by contract_id + provider_id
// and optionally a number. When applicationNo is empty, returns the
// primary application for the contract+provider. Returns ErrNotFound if
// no match.
func (c *Client) ResolveApplicationID(ctx context.Context, contractID, providerID, applicationNo string) (string, error) {
	filter := fmt.Sprintf(`contract_id="%s" && provider_id="%s" && is_deleted!=true`, contractID, providerID)
	if applicationNo != "" {
		filter += fmt.Sprintf(` && number="%s"`, applicationNo)
	}
	res, err := c.List(ctx, "applications", filter, 50)
	if err != nil {
		return "", err
	}
	if res.Total == 0 {
		return "", ErrNotFound
	}
	if res.Total > 1 {
		// Prefer the primary one.
		for _, item := range res.Items {
			if p, _ := item["is_primary"].(bool); p {
				return item["id"].(string), nil
			}
		}
		return "", fmt.Errorf("несколько заявок для поставщика %q", applicationNo)
	}
	return res.Items[0]["id"].(string), nil
}

// FindPendingCorrection returns the latest pending application_correction
// for an application_id, or ErrNotFound.
func (c *Client) FindPendingCorrection(ctx context.Context, applicationID string) (Record, error) {
	res, err := c.List(ctx, "application_corrections",
		fmt.Sprintf(`application_id="%s" && status="pending"`, applicationID), 1)
	if err != nil {
		return nil, err
	}
	if res.Total == 0 || len(res.Items) == 0 {
		return nil, ErrNotFound
	}
	return res.Items[0], nil
}

// GetContract loads a contract record.
func (c *Client) GetContract(ctx context.Context, contractID string) (Record, error) {
	return c.GetOne(ctx, "contracts", contractID)
}

// --- errors ---

// APIError is returned for PB HTTP >= 400 responses. Body holds the
// parsed PB error message (first data.message if present, else raw).
type APIError struct {
	Collection string
	Status     int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("pb %s: status %d: %s", e.Collection, e.Status, e.Body)
}

// IsNotFound reports whether the error is a 404 from PB.
func IsNotFound(err error) bool {
	if api, ok := err.(*APIError); ok {
		return api.Status == http.StatusNotFound
	}
	return err == ErrNotFound
}

// ErrNotFound is returned by lookup helpers when no record matches.
var ErrNotFound = fmt.Errorf("не найдено")

// parsePBError extracts a human-readable message from a PB error body.
func parsePBError(b []byte) string {
	var raw struct {
		Message string `json:"message"`
		Data    map[string]struct {
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(b, &raw); err == nil {
		if raw.Message != "" && raw.Data == nil {
			return raw.Message
		}
		var msgs []string
		for _, v := range raw.Data {
			if v.Message != "" {
				msgs = append(msgs, v.Message)
			}
		}
		if len(msgs) > 0 {
			return strings.Join(msgs, "; ")
		}
		if raw.Message != "" {
			return raw.Message
		}
	}
	s := string(b)
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}
