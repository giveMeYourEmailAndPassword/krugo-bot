package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// BridgeClient calls the Hermes Agent proxy sidecar via HTTP.
type BridgeClient struct {
	baseURL string
	apiKey  string
	client  *http.Client
	log     *slog.Logger
}

// NewBridgeClient creates a client for the Hermes proxy.
func NewBridgeClient(log *slog.Logger) *BridgeClient {
	return &BridgeClient{
		baseURL: envOrDefault("HERMES_BRIDGE_URL", "http://127.0.0.1:8080"),
		apiKey:  os.Getenv("HERMES_BRIDGE_KEY"),
		client:  &http.Client{Timeout: 30 * time.Minute},
		log:     log,
	}
}

type bridgeRequest struct {
	Prompt string `json:"prompt"`
}

type bridgeResponse struct {
	Text   string `json:"text"`
	Error  string `json:"error"`
	Status int    `json:"status"`
}

// Analyze sends the request to Hermes Agent and returns the raw response text.
// operationPrefix is the Telegram chat_id:message_id — a guaranteed-unique
// dedup key (the SQLite unique index). It is injected into the prompt so
// Hermes uses it as the operation_id prefix for idempotent tool calls
// (e.g. "12345:67890:payment:1"). This survives retries of the same
// Telegram message and never collides with a different message.
// Returns (text, nil) on success, ("", error) on failure.
func (b *BridgeClient) Analyze(ctx context.Context, operationPrefix, rawText string) (string, error) {
	if b.apiKey == "" {
		return "", fmt.Errorf("HERMES_BRIDGE_KEY is not set")
	}

	prompt := fmt.Sprintf(
		"Ты Hermes Agent. Выполни задачу из заявки менеджера, используя инструменты (скрипты в skills/contracts/tools/):\n\n%s\n\n"+
		"ID этой заявки (operation_id prefix): %s\n"+
		"ВАЖНО: каждый вызов инструмента ДОЛЖЕН включать operation_id вида \"%s:<секция>:<индекс>\" (например \"%s:payment:1\").\n"+
		"Это обеспечивает идемпотентность при повторных попытках.\n\n"+
		"Порядок действий:\n"+
		"1. Сделай GET договора — проверь статусы (is_cancelled, is_deleted, is_rejected, finance_status)\n"+
		"2. Для каждой секции заявки вызови соответствующий скрипт-инструмент\n"+
		"3. Покажи результат по каждой секции\n"+
		"4. Не спрашивай про клиента/проект/срочность/дедлайн — эти поля не нужны\n"+
		"5. Не классифицируй заявку — выполняй её",
		rawText, operationPrefix, operationPrefix, operationPrefix,
	)

	reqBody := bridgeRequest{Prompt: prompt}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/api/oneshot", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	b.log.Debug("sending to hermes", "url", req.URL.String(), "bytes", len(bodyBytes))

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("hermes bridge: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)

	var br bridgeResponse
	if err := json.Unmarshal(respBytes, &br); err != nil {
		return "", fmt.Errorf("parse bridge response: %w", err)
	}

	if br.Status != 200 {
		return "", fmt.Errorf("hermes bridge error: %s", br.Error)
	}

	b.log.Debug("hermes response", "bytes", len(br.Text))
	return br.Text, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
