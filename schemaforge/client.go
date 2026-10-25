package schemaforge

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	DatabaseURL  string
	GeminiAPIKey string
}

type Client struct {
	db        *pgxpool.Pool
	geminiKey string
	store     previewStore
}

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

	return &Client{
		db:        db,
		geminiKey: strings.TrimSpace(cfg.GeminiAPIKey),
		store: previewStore{
			previews: make(map[string]pendingPreview),
		},
	}, nil
}

func (c *Client) Close() {
	if c != nil && c.db != nil {
		c.db.Close()
	}
}
