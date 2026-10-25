package schemaforge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Config contains the settings required to initialize a SchemaForge client.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string

	// GeminiAPIKey is the API key used for Google Gemini requests.
	GeminiAPIKey string
}

// Client provides AI-assisted database action preview and execution.
// Create a client with New or NewWithPool and release its resources with Close.
type Client struct {
	db        *pgxpool.Pool
	geminiKey string
	store     previewStore
	ownsDB    bool
}

// New validates the configuration, connects to PostgreSQL, and returns
// a client that owns its connection pool.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		return nil, errors.New("DatabaseURL is required")
	}

	if strings.TrimSpace(cfg.GeminiAPIKey) == "" {
		return nil, errors.New("GeminiAPIKey is required")
	}

	dbConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	dbConfig.MaxConns = 10

	db, err := pgxpool.NewWithConfig(context.Background(), dbConfig)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, err
	}

	client, err := newClient(db, cfg.GeminiAPIKey, true)
	if err != nil {
		db.Close()
		return nil, err
	}

	return client, nil
}

// NewWithPool creates a SchemaForge client using an existing PostgreSQL pool.
// The caller retains ownership of the pool and must close it separately.
// NewWithPool verifies that the pool is reachable before returning.
func NewWithPool(db *pgxpool.Pool, geminiAPIKey string) (*Client, error) {
	if db == nil {
		return nil, errors.New("database pool is required")
	}

	if strings.TrimSpace(geminiAPIKey) == "" {
		return nil, errors.New("GeminiAPIKey is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.Ping(ctx); err != nil {
		return nil, err
	}

	return newClient(db, geminiAPIKey, false)
}

func newClient(db *pgxpool.Pool, geminiAPIKey string, ownsDB bool) (*Client, error) {
	return &Client{
		db:        db,
		geminiKey: strings.TrimSpace(geminiAPIKey),
		ownsDB:    ownsDB,
		store: previewStore{
			previews: make(map[string]pendingPreview),
		},
	}, nil
}

// Close releases resources owned by the client.
// When created with NewWithPool, the caller's pool is not closed.
func (c *Client) Close() {
	if c != nil && c.ownsDB && c.db != nil {
		c.db.Close()
	}
}
