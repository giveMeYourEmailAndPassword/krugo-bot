package telegram

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"gopkg.in/telebot.v3"

	"github.com/amantur/krugo-bot/internal/hermes"
	"github.com/amantur/krugo-bot/internal/rules"
	"github.com/amantur/krugo-bot/internal/tasks"
)

// Store is the subset of storage operations needed by telegram handlers.
type Store interface {
	Create(r *tasks.Request) error
	GetByID(id string) (*tasks.Request, error)
	UpdateStatus(id, status string) error
	UpdateAnalysis(id string, r *tasks.Request) error
}
// Bot orchestrates the Telegram side of Hermes.
type Bot struct {
	tele        *telebot.Bot
	store       Store
	hermesClient *hermes.BridgeClient
	log         *slog.Logger
}

func NewBot(tele *telebot.Bot, store Store, hermesClient *hermes.BridgeClient, log *slog.Logger) *Bot {
	b := &Bot{tele: tele, store: store, hermesClient: hermesClient, log: log}
	b.registerHandlers()
	return b
}


func (b *Bot) registerHandlers() {
	b.tele.Handle(telebot.OnText, b.handleText)
	b.tele.Handle(telebot.OnCallback, b.handleCallback)
}

// handleText processes incoming group text messages.
func (b *Bot) handleText(c telebot.Context) error {
	text := c.Text()

	// /status HERMES-XXXX — check request status
	if strings.HasPrefix(strings.ToLower(text), "/status") {
		return b.handleStatus(c, text)
	}

	if !rules.LooksLikeRequest(text) {
		return nil
	}

	chat := c.Chat()
	sender := c.Sender()

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

	if err := b.store.Create(req); err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	ack := fmt.Sprintf(
		"Принял заявку в работу.\n\nID: %s\nСтатус: анализирую",
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
		b.log.Error("hermes analysis failed", "request_id", req.ID, "error", err)
		req.Status = tasks.StatusHermesFailed
		req.Recommendation = "Ошибка: " + err.Error()
		_ = b.store.UpdateAnalysis(req.ID, req)
		b.sendStatus(req)
		return
	}

	req.Recommendation = result
	req.Status = tasks.StatusHermesResponded
	_ = b.store.UpdateAnalysis(req.ID, req)
	b.sendStatus(req)
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

	if req.NeedsClarification {
		sb.WriteString("\n<b>Нужны уточнения:</b> да\n")
		for _, q := range req.ClarificationQuestions {
			sb.WriteString(fmt.Sprintf("  • %s\n", html.EscapeString(q)))
		}
	}

	if req.Recommendation != "" {
		sb.WriteString(fmt.Sprintf("\n<b>Рекомендация:</b>\n%s\n", html.EscapeString(req.Recommendation)))
	}

	return sb.String()
}

// handleCallback processes inline button presses.
func (b *Bot) handleCallback(c telebot.Context) error {
	data := c.Callback().Data
	if !strings.HasPrefix(data, "action:") {
		return nil
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
	}
	if l, ok := labels[s]; ok {
		return l
	}
	return s
}

// handleStatus responds to /status HERMES-XXXX.
func (b *Bot) handleStatus(c telebot.Context, text string) error {
	parts := strings.Fields(text)
	if len(parts) < 2 {
		return c.Reply("Укажите ID заявки: /status HERMES-XXXX")
	}

	reqID := strings.ToUpper(parts[1])
	req, err := b.store.GetByID(reqID)
	if err != nil {
		return c.Reply("Заявка " + reqID + " не найдена.")
	}

	msg := formatStatus(req)
	return c.Reply(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}

func generateID() string {
	return fmt.Sprintf("HERMES-%d", time.Now().UnixNano()%100000)
}
