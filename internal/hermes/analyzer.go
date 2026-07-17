package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Analyzer sends request text to the Hermes AI (OpenAI-compatible API)
// and returns a structured AnalysisResponse.
type Analyzer struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
	log     *slog.Logger
}

// NewAnalyzer creates an Analyzer configured for an OpenAI-compatible endpoint.
func NewAnalyzer(apiKey, baseURL, model string, log *slog.Logger) *Analyzer {
	return &Analyzer{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 30 * time.Second},
		log:     log,
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// Analyze sends the raw request text to the AI and parses the response.
func (a *Analyzer) Analyze(ctx context.Context, rawText string) (*AnalysisResponse, error) {
	reqBody := chatRequest{
		Model: a.model,
		Messages: []chatMessage{
			{Role: "system", Content: SystemPrompt()},
			{Role: "user", Content: rawText},
		},
		Temperature: 0.2,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := a.baseURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	a.log.Debug("sending to AI", "url", url, "model", a.model)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		a.log.Error("AI API error", "status", resp.StatusCode, "body", string(respBytes))
		return nil, fmt.Errorf("AI API returned %d: %s", resp.StatusCode, string(respBytes))
	}

	var cr chatResponse
	if err := json.Unmarshal(respBytes, &cr); err != nil {
		return nil, fmt.Errorf("unmarshal chat response: %w", err)
	}

	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("empty choices from AI")
	}

	content := cr.Choices[0].Message.Content
	a.log.Debug("AI response", "content", content)

	var result AnalysisResponse
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		// The AI sometimes wraps JSON in ``` — try stripping markdown fences.
		content = stripMarkdownFences(content)
		if err2 := json.Unmarshal([]byte(content), &result); err2 != nil {
			return nil, fmt.Errorf("unmarshal analysis: %w (raw: %s)", err, content)
		}
	}

	return &result, nil
}

func stripMarkdownFences(s string) string {
	s = bytesTrimPrefix(s, "```json\n")
	s = bytesTrimPrefix(s, "```\n")
	s = bytesTrimSuffix(s, "\n```")
	s = bytesTrimSuffix(s, "```")
	return s
}

func bytesTrimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func bytesTrimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}
