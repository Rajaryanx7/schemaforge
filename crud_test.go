package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestCRUDRecords(t *testing.T) {
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set; skipping database integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := connectDB(ctx)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	const table = "schemaforge_crud_test"

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schemaforge_crud_test (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL,
			is_completed BOOLEAN NOT NULL DEFAULT FALSE
		)
	`)
	if err != nil {
		t.Fatalf("create test table: %v", err)
	}

	// Ensure this test's table is removed afterward.
	defer func() {
		_, _ = pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS schemaforge_crud_test`)
	}()

	s := &server{db: pool}

	// POST: insert a record.
	payload := []byte(`{"title":"CRUD integration test","is_completed":false}`)
	req := httptest.NewRequest(http.MethodPost,
		"/records/"+table, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: expected 201, got %d: %s",
			rec.Code, rec.Body.String())
	}

	var inserted struct {
		Record map[string]any `json:"record"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &inserted); err != nil {
		t.Fatalf("decode POST response: %v", err)
	}

	id, ok := inserted.Record["id"].(float64)
	if !ok || id <= 0 {
		t.Fatalf("POST returned invalid record ID: %v", inserted.Record)
	}
	recordID := int64(id)

	// GET: verify the record appears in the list.
	req = httptest.NewRequest(http.MethodGet, "/records/"+table, nil)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s",
			rec.Code, rec.Body.String())
	}

	var records []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &records); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("GET: expected 1 record, got %d", len(records))
	}

	// PATCH: update the record.
	payload = []byte(`{"is_completed":true}`)
	req = httptest.NewRequest(http.MethodPatch,
		fmt.Sprintf("/records/%s/%d", table, recordID),
		bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d: %s",
			rec.Code, rec.Body.String())
	}

	var completed bool
	err = pool.QueryRow(ctx,
		`SELECT is_completed FROM schemaforge_crud_test WHERE id = $1`,
		recordID).Scan(&completed)
	if err != nil {
		t.Fatalf("verify PATCH: %v", err)
	}
	if !completed {
		t.Fatal("PATCH did not update is_completed to true")
	}

	// DELETE: remove the record.
	req = httptest.NewRequest(http.MethodDelete,
		fmt.Sprintf("/records/%s/%d", table, recordID), nil)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: expected 200, got %d: %s",
			rec.Code, rec.Body.String())
	}

	var count int
	err = pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM schemaforge_crud_test WHERE id = $1`,
		recordID).Scan(&count)
	if err != nil {
		t.Fatalf("verify DELETE: %v", err)
	}
	if count != 0 {
		t.Fatalf("DELETE failed: record still exists")
	}
}
