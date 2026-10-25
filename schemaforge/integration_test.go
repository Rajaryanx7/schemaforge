package schemaforge

import (
	"context"
	"fmt"
	"os"
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

		// Verify the database wasn't changed by the rejected call.
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
