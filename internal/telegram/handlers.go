package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"os"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/amantur/krugo-bot/internal/hermes"
	"github.com/amantur/krugo-bot/internal/rules"
	"github.com/amantur/krugo-bot/internal/tasks"
)


// Store is the subset of storage operations needed by telegram handlers.
type Store interface {
	Create(r *tasks.Request) (bool, error)
	GetByID(id string) (*tasks.Request, error)
	UpdateStatus(id, status string) error
	UpdateAnalysis(id string, r *tasks.Request) error
}


// Bot orchestrates the Telegram side of Hermes.
type Bot struct {
	tele         *telebot.Bot
	store        Store
	hermesClient *hermes.BridgeClient
	log          *slog.Logger
	allowedUsers map[int64]bool
}

func NewBot(tele *telebot.Bot, store Store, hermesClient *hermes.BridgeClient, log *slog.Logger, allowedIDs []int64) *Bot {
	b := &Bot{tele: tele, store: store, hermesClient: hermesClient, log: log, allowedUsers: make(map[int64]bool)}
	for _, id := range allowedIDs {
		b.allowedUsers[id] = true
	}
	b.registerHandlers()
	return b
}
func (b *Bot) registerHandlers() {
	b.tele.Handle("/start", b.handleStart)
	b.tele.Handle("/help", b.handleStart)
	b.tele.Handle(telebot.OnText, b.handleText)
	b.tele.Handle(telebot.OnCallback, b.handleCallback)
}

// handleStart responds to /start and /help.
func (b *Bot) handleStart(c telebot.Context) error {
	if !b.allowedUsers[c.Sender().ID] {
		return nil
	}
	msg := "Круго-Бот готов к работе.\n\n" +
		"Команды:\n" +
		"/start — это сообщение\n" +
		"/status KRUGOSVET-XXXXX — статус заявки\n" +
		"/history ID_ДОГОВОРА — история изменений договора\n\n" +
		"Или просто отправьте заявку по шаблону."
	return c.Send(msg, mainKeyboard())
}
// handleText processes incoming group text messages.
func (b *Bot) handleText(c telebot.Context) error {
	text := c.Text()
	sender := c.Sender()

	if !b.allowedUsers[sender.ID] {
		return nil
	}

	if strings.HasPrefix(strings.ToLower(text), "/history") {
		return b.handleHistory(c, text)
	}
	if strings.HasPrefix(strings.ToLower(text), "/status") {
		return b.handleStatus(c, text)
	}

	if !rules.LooksLikeRequest(text) {
		return nil
	}
	// Template validation — only for contract requests
	if strings.Contains(strings.ToLower(text), "договор") || strings.Contains(text, "baza.krugo.tours") {
		if msg := validateTemplate(text); msg != "" {
			return c.Reply("⚠️ " + msg)
		}
	}

	// Common dedup + request record. The SQLite (chat_id, message_id)
	// unique index prevents duplicate processing on retry/update.
	chat := c.Chat()
	req := &tasks.Request{
		ID:                generateID(),
		TelegramChatID:    chat.ID,
		TelegramMessageID: c.Message().ID,
		AuthorID:          sender.ID,
		AuthorUsername:    sender.Username,
		RawText:           text,
		Status:            tasks.StatusReceived,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	inserted, err := b.store.Create(req)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if !inserted {
		return nil // duplicate message, already processed
	}

	// Hermes relay: bot is a filter only — it validates the template,
	// deduplicates, and forwards to Hermes Agent which parses the
	// structured template and executes operations in PocketBase.
	ack := fmt.Sprintf(
		"Принял заявку в работу.\n\nID: %s\nСтатус: анализирую\n⏳ Ожидайте ответ Krugosvet Helper...",
		req.ID,
	)
	if err := c.Reply(ack); err != nil {
		return fmt.Errorf("reply ack: %w", err)
	}

	go b.analyzeRequest(req)
	return nil
}

func (b *Bot) analyzeRequest(req *tasks.Request) {
	req.Status = tasks.StatusInProgress
	_ = b.store.UpdateStatus(req.ID, req.Status)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	result, err := b.hermesClient.Analyze(ctx, req.RawText)
	if err != nil {
		b.log.Error("analysis failed", "request_id", req.ID, "error", err)
		req.Status = tasks.StatusHermesFailed
		req.Recommendation = "Ошибка: " + err.Error()
		_ = b.store.UpdateAnalysis(req.ID, req)
		b.sendStatus(req)
		return
	}

	req.Recommendation = cleanRecommendation(result)
	req.Status = tasks.StatusHermesResponded
	_ = b.store.UpdateAnalysis(req.ID, req)
	b.sendStatus(req)
}

func cleanRecommendation(text string) string {
	// Remove price split section
	re := regexp.MustCompile(`(?is)Price Split[^\n]*(\n[^\n]*)*`)
	text = re.ReplaceAllString(text, "")
	re = regexp.MustCompile(`(?is)price.split[^\n]*(\n[^\n]*)*`)
	text = re.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}

func (b *Bot) sendStatus(req *tasks.Request) {
	text := formatStatus(req)
	markup := mainKeyboard()

	_, err := b.tele.Send(
		&telebot.Chat{ID: req.TelegramChatID},
		text,
		markup,
		&telebot.SendOptions{ParseMode: telebot.ModeHTML},
	)
	if err != nil {
		b.tele.OnError(err, nil)
	}
}

func formatStatus(req *tasks.Request) string {
	var sb strings.Builder
	sb.WriteString("<b>Заявка обработана.</b>\n\n")
	sb.WriteString(fmt.Sprintf("ID: %s\n", req.ID))
	sb.WriteString(fmt.Sprintf("Статус: %s\n", statusLabel(req.Status)))

	if req.Client != "" {
		sb.WriteString(fmt.Sprintf("Клиент: %s\n", html.EscapeString(req.Client)))
	}
	if req.Project != "" {
		sb.WriteString(fmt.Sprintf("Проект: %s\n", html.EscapeString(req.Project)))
	}
	if req.RequestType != "" {
		sb.WriteString(fmt.Sprintf("Тип: %s\n", html.EscapeString(req.RequestType)))
	}
	if req.Relevance != "" {
		sb.WriteString(fmt.Sprintf("Актуальность: %s\n", html.EscapeString(req.Relevance)))
	}
	if req.Risk != "" {
		sb.WriteString(fmt.Sprintf("Риск: %s\n", html.EscapeString(req.Risk)))
	}

	if req.Recommendation != "" {
		escaped := html.EscapeString(req.Recommendation)
		boldRe := regexp.MustCompile(`\*\*(.+?)\*\*`)
		escaped = boldRe.ReplaceAllString(escaped, "<b>$1</b>")
		sb.WriteString(fmt.Sprintf("\n<b>Рекомендация:</b>\n%s\n", escaped))
	}

	if req.NeedsClarification {
		sb.WriteString("\n<b>Нужны уточнения:</b> да\n")
		for _, q := range req.ClarificationQuestions {
			sb.WriteString(fmt.Sprintf("  • %s\n", html.EscapeString(q)))
		}
	}

	if req.Recommendation != "" {
		sb.WriteString(fmt.Sprintf("\n<b>Рекомендация:</b>\n%s\n", req.Recommendation))
	}

	return sb.String()
}

// handleCallback processes inline button presses.
func (b *Bot) handleCallback(c telebot.Context) error {
	data := c.Callback().Data
	data = strings.TrimSpace(c.Callback().Data)


	if !b.allowedUsers[c.Callback().Sender.ID] {
		return c.Respond(&telebot.CallbackResponse{Text: "Нет доступа"})
	}
	// Template buttons
	if strings.HasPrefix(data, "tpl:") {
		return b.handleTemplate(c, data)
	}

	if !strings.HasPrefix(data, "action:") {
		return nil
	}

	if !b.allowedUsers[c.Callback().Sender.ID] {
		return c.Respond(&telebot.CallbackResponse{Text: "Нет доступа"})
	}

	action := strings.TrimPrefix(data, "action:")
	msgText := c.Callback().Message.Text
	reqID := extractRequestID(msgText)
	if reqID == "" {
		return c.Respond(&telebot.CallbackResponse{Text: "Не удалось найти ID заявки"})
	}

	req, err := b.store.GetByID(reqID)
	if err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Заявка не найдена"})
	}

	newStatus := action
	switch action {
	case "ready_for_dev":
		newStatus = tasks.StatusReadyForDev
	case "needs_clarification":
		newStatus = tasks.StatusNeedsClarification
	case "assigned":
		newStatus = tasks.StatusAssigned
	case "done":
		newStatus = tasks.StatusDone
	}

	if err := b.store.UpdateStatus(reqID, newStatus); err != nil {
		return c.Respond(&telebot.CallbackResponse{Text: "Ошибка обновления статуса"})
	}
	req.Status = newStatus

	label := statusLabel(newStatus)
	return c.Respond(&telebot.CallbackResponse{
		Text: fmt.Sprintf("Статус заявки %s: %s", reqID, label),
	})
}

func extractRequestID(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ID: ") {
			return strings.TrimPrefix(line, "ID: ")
		}
	}
	return ""
}

func statusLabel(s string) string {
	labels := map[string]string{
		tasks.StatusReceived:           "получена",
		tasks.StatusInProgress:         "анализирую",
		tasks.StatusNeedsClarification: "нужны уточнения",
		tasks.StatusReadyForReview:     "готово к проверке",
		tasks.StatusReadyForDev:        "готово к передаче",
		tasks.StatusAssigned:           "назначен",
		tasks.StatusDone:               "закрыто",
		tasks.StatusRejected:           "отклонено",
		tasks.StatusHermesResponded:    "Krugosvet Helper",
		tasks.StatusHermesFailed:       "ошибка Krugosvet Helper",
	}
	if l, ok := labels[s]; ok {
		return l
	}
	return s
}

// handleStatus responds to /status KRUGOSVET-XXXXX.
func (b *Bot) handleStatus(c telebot.Context, text string) error {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return c.Reply("Укажите ID заявки: /status KRUGOSVET-XXXXX")
	}

	reqID := strings.ToUpper(parts[1])
	req, err := b.store.GetByID(reqID)
	if err != nil {
		return c.Reply("Заявка " + reqID + " не найдена.")
	}

	msg := formatStatus(req)
	return c.Reply(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

// handleTemplate responds to template button callbacks.
func (b *Bot) handleTemplate(c telebot.Context, data string) error {
	switch data {
	case "tpl:contract_change":
		c.Respond(&telebot.CallbackResponse{Text: "Шаблон отправлен"})
		return c.Send(contractTemplate())
	}
	return c.Respond(&telebot.CallbackResponse{Text: "Шаблон не найден"})
}


// handleHistory responds to /history <contract_id>.
func (b *Bot) handleHistory(c telebot.Context, text string) error {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return c.Reply("Укажите ID договора: /history t85493bo3ky8ccs")
	}
	contractID := parts[1]
	pbURL := os.Getenv("PB_URL")
	pbUser := os.Getenv("PB_USER")
	pbPass := os.Getenv("PB_PASS")
	if pbURL == "" || pbUser == "" || pbPass == "" {
		return c.Reply("PB_URL, PB_USER, PB_PASS не заданы")
	}
	token, err := getPBToken(pbURL, pbUser, pbPass)
	if err != nil {
		return c.Reply("Ошибка доступа к базе")
	}
	records, err := getAuditLog(pbURL, token, contractID)
	if err != nil || len(records) == 0 {
		return c.Reply("Нет записей для договора " + contractID)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("История %s:\n\n", contractID))
	for _, r := range records {
		created := ""
		if c, ok := r["created"].(string); ok && len(c) >= 19 {
			created = strings.Replace(c[:19], "T", " ", 1)
		}
		action := ""
		if a, ok := r["action"].(string); ok {
			action = a
		}
		desc := ""
		if d, ok := r["description"]; ok && d != nil {
			if ds, ok := d.(string); ok {
				desc = ds
			}
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", created, action, desc))
	}
	return c.Reply(sb.String())
}

func getPBToken(pbURL, user, pass string) (string, error) {
	body, _ := json.Marshal(map[string]string{"identity": user, "password": pass})
	resp, err := http.Post(pbURL+"/api/collections/_superusers/auth-with-password", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	if t, ok := result["token"]; ok {
		return t.(string), nil
	}
	return "", fmt.Errorf("auth failed: %d", resp.StatusCode)
}

func getAuditLog(pbURL, token, contractID string) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/collections/contract_audit_log/records?perPage=20&sort=-created&filter=(contract_id=\"%s\")", pbURL, contractID)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)
	items, _ := result["items"].([]interface{})
	var records []map[string]interface{}
	for _, item := range items {
		records = append(records, item.(map[string]interface{}))
	}
	return records, nil
}

func generateID() string {
	return fmt.Sprintf("KRUGOSVET-%d", time.Now().UnixNano()%100000)
}
