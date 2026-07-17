package tasks

import "time"

// Status constants for request lifecycle.
const (
	StatusReceived          = "received"
	StatusInProgress         = "in_progress"
	StatusHermesResponded    = "hermes_responded"
	StatusHermesFailed       = "hermes_failed"
	StatusNeedsClarification = "needs_clarification"
	StatusRejected          = "rejected"
	StatusReadyForReview     = "ready_for_review"
	StatusReadyForDev        = "ready_for_dev"
	StatusAssigned           = "assigned"
	StatusDone              = "done"
)

// AllStatuses returns every valid status in lifecycle order.
func AllStatuses() []string {
	return []string{
		StatusReceived,
		StatusInProgress,
		StatusNeedsClarification,
		StatusReadyForReview,
		StatusReadyForDev,
		StatusAssigned,
		StatusDone,
		StatusRejected,
	}
}

// Request represents a client request filed through Telegram.
type Request struct {
	ID         string `json:"id"`
	TelegramChatID    int64  `json:"telegram_chat_id"`
	TelegramMessageID int    `json:"telegram_message_id"`
	AuthorID          int64  `json:"author_id"`
	AuthorUsername    string `json:"author_username"`
	RawText           string `json:"raw_text"`

	Client      string `json:"client"`
	Project     string `json:"project"`
	RequestType string `json:"request_type"`
	Description string `json:"description"`

	Status    string `json:"status"`
	Relevance string `json:"relevance"`
	Risk      string `json:"risk"`

	NeedsClarification      bool     `json:"needs_clarification"`
	ClarificationQuestions  []string `json:"clarification_questions"`
	Recommendation          string   `json:"recommendation"`
	Summary                 string   `json:"summary"`
	NextAction              string   `json:"next_action"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// IsTerminal reports whether the request is in a terminal state.
func (r *Request) IsTerminal() bool {
	return r.Status == StatusDone || r.Status == StatusRejected
}
