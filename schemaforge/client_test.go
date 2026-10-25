package schemaforge

import (
	"os"
	"strings"
	"testing"
)

func TestNewConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name:    "missing database URL",
			config:  Config{GeminiAPIKey: "test-key"},
			wantErr: "DatabaseURL is required",
		},
		{
			name:    "blank database URL",
			config:  Config{DatabaseURL: "   ", GeminiAPIKey: "test-key"},
			wantErr: "DatabaseURL is required",
		},
		{
			name:    "missing Gemini API key",
			config:  Config{DatabaseURL: "postgres://localhost/test"},
			wantErr: "GeminiAPIKey is required",
		},
		{
			name:    "blank Gemini API key",
			config:  Config{DatabaseURL: "postgres://localhost/test", GeminiAPIKey: "   "},
			wantErr: "GeminiAPIKey is required",
		},
		{
			name:    "invalid database URL",
			config:  Config{DatabaseURL: "://invalid", GeminiAPIKey: "test-key"},
			wantErr: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.config)

			if err == nil {
				if client != nil {
					client.Close()
				}
				t.Fatal("expected an error")
			}

			if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNewWithPoolValidation(t *testing.T) {
	t.Run("nil pool", func(t *testing.T) {
		_, err := NewWithPool(nil, "test-key")
		if err == nil || err.Error() != "database pool is required" {
			t.Fatalf("expected database pool error, got %v", err)
		}
	})

	t.Run("missing API key", func(t *testing.T) {
		_, err := NewWithPool(nil, "")
		if err == nil || err.Error() != "database pool is required" {
			t.Fatalf("expected pool validation to happen first, got %v", err)
		}
	})
}
func TestClientClose(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var client *Client
		client.Close() // Must not panic.
	})

	t.Run("client with no database", func(t *testing.T) {
		client := &Client{}
		client.Close() // Must not panic.
	})

	t.Run("borrowed database pool remains open", func(t *testing.T) {
		client := &Client{
			ownsDB: false,
		}
		client.Close() // Must not attempt to close a borrowed pool.
	})
}
func TestNewIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	client, err := New(Config{
		DatabaseURL:  databaseURL,
		GeminiAPIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}

	if client == nil {
		t.Fatal("New() returned a nil client")
	}

	if !client.ownsDB {
		t.Error("New() client should own its database pool")
	}

	if client.geminiKey != "test-key" {
		t.Error("New() did not store the API key correctly")
	}

	client.Close()
}
