package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/amantur/krugo-bot/internal/tasks"
)

// SQLiteStore implements Store backed by a local SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens the SQLite database and runs the schema migration.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return s, nil
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// migrate creates tables that don't yet exist.
func (s *SQLiteStore) migrate() error {
	query := `
	CREATE TABLE IF NOT EXISTS requests (
		id TEXT PRIMARY KEY,
		telegram_chat_id INTEGER NOT NULL,
		telegram_message_id INTEGER NOT NULL,
		author_id INTEGER,
		author_username TEXT,
		raw_text TEXT NOT NULL,
		client TEXT,
		project TEXT,
		request_type TEXT,
		description TEXT,
		status TEXT NOT NULL DEFAULT 'received',
		relevance TEXT,
		risk TEXT,
		needs_clarification BOOLEAN DEFAULT FALSE,
		clarification_questions TEXT,
		summary TEXT,
		recommendation TEXT,
		next_action TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);
	`
	_, err := s.db.Exec(query)
	if err != nil {
		return err
	}

	// Clean up existing duplicates before creating unique index
	s.db.Exec(`DELETE FROM requests WHERE rowid NOT IN (SELECT MIN(rowid) FROM requests GROUP BY telegram_chat_id, telegram_message_id)`)

	_, err = s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_requests_msg ON requests(telegram_chat_id, telegram_message_id)`)
	return err
}

// Create inserts a new request.
func (s *SQLiteStore) Create(r *tasks.Request) (bool, error) {
	questionsJSON, _ := json.Marshal(r.ClarificationQuestions)

	result, err := s.db.Exec(
		`INSERT INTO requests (
			id, telegram_chat_id, telegram_message_id, author_id, author_username,
			raw_text, client, project, request_type, description,
			status, relevance, risk,
			needs_clarification, clarification_questions, summary, recommendation, next_action,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(telegram_chat_id, telegram_message_id) DO NOTHING`,
		r.ID, r.TelegramChatID, r.TelegramMessageID, r.AuthorID, r.AuthorUsername,
		r.RawText, r.Client, r.Project, r.RequestType, r.Description,
		r.Status, r.Relevance, r.Risk,
		r.NeedsClarification, string(questionsJSON), r.Summary, r.Recommendation, r.NextAction,
		r.CreatedAt, r.UpdatedAt,
	)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

// GetByID looks up a request.
func (s *SQLiteStore) GetByID(id string) (*tasks.Request, error) {
	var r tasks.Request
	var questionsJSON string

	err := s.db.QueryRow(
		`SELECT id, telegram_chat_id, telegram_message_id, author_id, author_username,
			raw_text, client, project, request_type, description,
			status, relevance, risk,
			needs_clarification, clarification_questions, summary, recommendation, next_action,
			created_at, updated_at
		FROM requests WHERE id = ?`, id,
	).Scan(
		&r.ID, &r.TelegramChatID, &r.TelegramMessageID, &r.AuthorID, &r.AuthorUsername,
		&r.RawText, &r.Client, &r.Project, &r.RequestType, &r.Description,
		&r.Status, &r.Relevance, &r.Risk,
		&r.NeedsClarification, &questionsJSON, &r.Summary, &r.Recommendation, &r.NextAction,
		&r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if questionsJSON != "" {
		_ = json.Unmarshal([]byte(questionsJSON), &r.ClarificationQuestions)
	}

	return &r, nil
}

// UpdateStatus sets the status and bumps updated_at.
func (s *SQLiteStore) UpdateStatus(id, status string) error {
	_, err := s.db.Exec(
		`UPDATE requests SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), id,
	)
	return err
}

// UpdateAnalysis applies Hermes analysis results to an existing request.
func (s *SQLiteStore) UpdateAnalysis(id string, r *tasks.Request) error {
	questionsJSON, _ := json.Marshal(r.ClarificationQuestions)

	_, err := s.db.Exec(
		`UPDATE requests SET
			client = ?, project = ?, request_type = ?, description = ?,
			relevance = ?, risk = ?,
			needs_clarification = ?, clarification_questions = ?,
			summary = ?, recommendation = ?, next_action = ?,
			status = ?, updated_at = ?
		WHERE id = ?`,
		r.Client, r.Project, r.RequestType, r.Description,
		r.Relevance, r.Risk,
		r.NeedsClarification, string(questionsJSON),
		r.Summary, r.Recommendation, r.NextAction,
		r.Status, time.Now(),
		id,
	)
	return err
}
