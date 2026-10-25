package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestDeleteRecord(t *testing.T) {
	if err := os.Setenv("DATABASE_URL", os.Getenv("DATABASE_URL")); err != nil {
		t.Fatal(err)
	}

	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set; skipping database integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := connectDB(ctx)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	// Create a uniquely named test table.
	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schemaforge_delete_test (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}
	defer pool.Exec(context.Background(), `DROP TABLE IF EXISTS schemaforge_delete_test`)

	var id int64
	err = pool.QueryRow(ctx,
		`INSERT INTO schemaforge_delete_test (title) VALUES ($1) RETURNING id`,
		"temporary test record",
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert test record: %v", err)
	}

	s := &server{db: pool}
	req := httptest.NewRequest(
		http.MethodDelete,
		"/records/schemaforge_delete_test/"+jsonNumber(id),
		nil,
	)
	rec := httptest.NewRecorder()

	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schemaforge_delete_test WHERE id = $1`, id,
	).Scan(&count)
	if err != nil {
		t.Fatalf("verify deletion: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected record to be deleted; found %d matching records", count)
	}
}

func jsonNumber(n int64) string {
	return json.Number(stringInt(n)).String()
}

func stringInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
