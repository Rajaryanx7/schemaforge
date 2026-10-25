package schemaforge

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	apiKey := os.Getenv("GEMINI_API_KEY")

	if databaseURL == "" || apiKey == "" {
		t.Skip("set DATABASE_URL and GEMINI_API_KEY to run PostgreSQL integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	client, err := NewWithPool(pool, apiKey)
	if err != nil {
		t.Fatalf("initialize client: %v", err)
	}
	defer client.Close()

	table := fmt.Sprintf("sf_it_%d", time.Now().UnixNano())
	quotedTable := quoteIdent(table)

	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%s (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL,
			is_completed BOOLEAN NOT NULL DEFAULT FALSE
		)`, quotedTable))
	if err != nil {
		t.Fatalf("create temporary test table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS public."+quotedTable)
	}()

	var recordID int64
	err = pool.QueryRow(ctx,
		"INSERT INTO public."+quotedTable+" (title) VALUES ($1) RETURNING id",
		"integration test",
	).Scan(&recordID)
	if err != nil {
		t.Fatalf("insert test record: %v", err)
	}

	t.Run("successful update", func(t *testing.T) {
		action := Action{
			Type:  ActionUpdate,
			Table: table,
			ID:    recordID,
			Values: map[string]any{
				"is_completed": true,
			},
		}

		token, err := client.issuePreview(action)
		if err != nil {
			t.Fatalf("issue preview token: %v", err)
		}

		result, err := client.Execute(ctx, action, token, true)
		if err != nil {
			t.Fatalf("execute update: %v", err)
		}

		if result["is_completed"] != true {
			t.Fatalf("expected updated record to be completed, got %#v", result)
		}
	})

	t.Run("successful insert", func(t *testing.T) {
		action := Action{
			Type:  ActionInsert,
			Table: table,
			Values: map[string]any{
				"title":        "insert integration test",
				"is_completed": false,
			},
		}

		token, err := client.issuePreview(action)
		if err != nil {
			t.Fatalf("issue preview token: %v", err)
		}

		result, err := client.Execute(ctx, action, token, true)
		if err != nil {
			t.Fatalf("execute insert: %v", err)
		}

		if result["title"] != "insert integration test" {
			t.Fatalf("unexpected insert result: %#v", result)
		}
	})

	t.Run("successful delete", func(t *testing.T) {
		var id int64
		err := pool.QueryRow(ctx,
			"INSERT INTO public."+quotedTable+" (title) VALUES ($1) RETURNING id",
			"delete integration test",
		).Scan(&id)
		if err != nil {
			t.Fatalf("create record for deletion: %v", err)
		}

		action := Action{
			Type:  ActionDelete,
			Table: table,
			ID:    id,
		}

		token, err := client.issuePreview(action)
		if err != nil {
			t.Fatalf("issue preview token: %v", err)
		}

		result, err := client.Execute(ctx, action, token, true)
		if err != nil {
			t.Fatalf("execute delete: %v", err)
		}

		if result["id"] == nil {
			t.Fatalf("expected deleted record ID, got %#v", result)
		}
	})

	t.Run("token reuse rejected", func(t *testing.T) {
		action := Action{
			Type:  ActionUpdate,
			Table: table,
			ID:    recordID,
			Values: map[string]any{
				"is_completed": false,
			},
		}

		token, err := client.issuePreview(action)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := client.Execute(ctx, action, token, true); err != nil {
			t.Fatalf("first execution failed: %v", err)
		}

		if _, err := client.Execute(ctx, action, token, true); err == nil {
			t.Fatal("expected reused token to be rejected")
		}
	})

	t.Run("confirmation required", func(t *testing.T) {
		action := Action{
			Type:  ActionUpdate,
			Table: table,
			ID:    recordID,
			Values: map[string]any{
				"is_completed": false,
			},
		}

		token, err := client.issuePreview(action)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := client.Execute(ctx, action, token, false); err == nil {
			t.Fatal("expected execution without confirmation to be rejected")
		}

		var completed bool
		err = pool.QueryRow(ctx,
			"SELECT is_completed FROM public."+quotedTable+" WHERE id = $1",
			recordID,
		).Scan(&completed)
		if err != nil {
			t.Fatal(err)
		}

		if completed {
			t.Fatal("record changed despite missing confirmation")
		}
	})
}

func TestPreviewIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DATABASE_URL to run Preview integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	table := fmt.Sprintf("sf_preview_%d", time.Now().UnixNano())
	quotedTable := quoteIdent(table)

	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%s (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL
		)`, quotedTable))
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS public."+quotedTable)
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Errorf("expected test API key header")
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}

		actionJSON := fmt.Sprintf(
			`{"action":"insert","table":%q,"values":{"title":"preview test"}}`,
			table,
		)
		if r.URL.Query().Get("case") == "invalid" {
			actionJSON = `{"action":"unsupported","table":"students"}`
		}

		response, _ := json.Marshal(map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]string{"text": actionJSON},
						},
					},
				},
			},
		})

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	defer server.Close()

	oldEndpoint := geminiEndpoint
	oldClient := geminiHTTPClient
	geminiEndpoint = server.URL
	geminiHTTPClient = server.Client()
	defer func() {
		geminiEndpoint = oldEndpoint
		geminiHTTPClient = oldClient
	}()

	client, err := NewWithPool(pool, "test-key")
	if err != nil {
		t.Fatalf("initialize client: %v", err)
	}
	defer client.Close()

	t.Run("empty instruction", func(t *testing.T) {
		_, _, err := client.Preview(ctx, "   ")
		if err == nil || err.Error() != "instruction cannot be empty" {
			t.Fatalf("expected empty instruction error, got %v", err)
		}
	})

	t.Run("oversized instruction", func(t *testing.T) {
		_, _, err := client.Preview(ctx, strings.Repeat("a", 2001))
		if err == nil || err.Error() != "instruction cannot exceed 2000 characters" {
			t.Fatalf("expected length error, got %v", err)
		}
	})

	t.Run("successful preview", func(t *testing.T) {
		action, token, err := client.Preview(ctx, "add a preview test")
		if err != nil {
			t.Fatalf("Preview returned error: %v", err)
		}
		if action == nil || action.Type != ActionInsert || action.Table != table {
			t.Fatalf("unexpected action: %#v", action)
		}
		if token == "" {
			t.Fatal("expected preview token")
		}
	})

	t.Run("invalid AI action", func(t *testing.T) {
		originalEndpoint := geminiEndpoint
		geminiEndpoint = server.URL + "?case=invalid"
		defer func() { geminiEndpoint = originalEndpoint }()

		_, _, err := client.Preview(ctx, "do something unsupported")
		if err == nil {
			t.Fatal("expected invalid action to be rejected")
		}
	})
}
func TestValidateActionIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set DATABASE_URL to run validation integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("ping PostgreSQL: %v", err)
	}

	table := fmt.Sprintf("sf_validate_%d", time.Now().UnixNano())
	quotedTable := quoteIdent(table)

	_, err = pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE public.%s (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL,
			quantity INTEGER NOT NULL DEFAULT 0
		)`, quotedTable))
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(),
			"DROP TABLE IF EXISTS public."+quotedTable)
	}()

	client, err := NewWithPool(pool, "test-key")
	if err != nil {
		t.Fatalf("initialize client: %v", err)
	}
	defer client.Close()

	tests := []struct {
		name    string
		action  *Action
		wantErr string
	}{
		{
			name:    "nil action",
			action:  nil,
			wantErr: "action is required",
		},
		{
			name: "invalid table identifier",
			action: &Action{
				Type: ActionInsert, Table: "Bad-Table",
				Values: map[string]any{"title": "test"},
			},
			wantErr: "invalid identifier",
		},
		{
			name: "table does not exist",
			action: &Action{
				Type: ActionInsert, Table: "sf_missing_table",
				Values: map[string]any{"title": "test"},
			},
			wantErr: "does not exist",
		},
		{
			name: "insert without values",
			action: &Action{
				Type: ActionInsert, Table: table,
			},
			wantErr: "requires values",
		},
		{
			name: "invalid column identifier",
			action: &Action{
				Type: ActionInsert, Table: table,
				Values: map[string]any{"Bad-Column": "test"},
			},
			wantErr: "invalid identifier",
		},
		{
			name: "unknown column",
			action: &Action{
				Type: ActionInsert, Table: table,
				Values: map[string]any{"missing_column": "test"},
			},
			wantErr: "does not exist",
		},
		{
			name: "identity column cannot be set",
			action: &Action{
				Type: ActionInsert, Table: table,
				Values: map[string]any{"id": 10},
			},
			wantErr: "cannot set identity column",
		},
		{
			name: "invalid column value type",
			action: &Action{
				Type: ActionInsert, Table: table,
				Values: map[string]any{"quantity": "not-a-number"},
			},
			wantErr: "invalid value for column",
		},
		{
			name: "update requires positive ID",
			action: &Action{
				Type: ActionUpdate, Table: table, ID: 0,
				Values: map[string]any{"title": "updated"},
			},
			wantErr: "positive ID",
		},
		{
			name: "delete requires positive ID",
			action: &Action{
				Type: ActionDelete, Table: table, ID: 0,
			},
			wantErr: "positive ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := client.validateAction(ctx, tt.action)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}

	t.Run("valid insert action", func(t *testing.T) {
		action := &Action{
			Type: ActionInsert, Table: table,
			Values: map[string]any{"title": "valid", "quantity": 3},
		}
		if err := client.validateAction(ctx, action); err != nil {
			t.Fatalf("expected valid action, got %v", err)
		}
	})
}
