package storage

import "github.com/amantur/krugo-bot/internal/tasks"

// Store is the persistence interface for requests.
type Store interface {
	// Create persists a new request.
	Create(r *tasks.Request) error

	// GetByID retrieves a request by its Hermes ID.
	GetByID(id string) (*tasks.Request, error)

	// UpdateStatus sets a new status and updates the timestamp.
	UpdateStatus(id, status string) error

	// UpdateAnalysis saves Hermes analysis results.
	UpdateAnalysis(id string, r *tasks.Request) error
}
