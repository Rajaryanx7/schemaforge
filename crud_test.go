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

const crudTestTable = "schemaforge_crud_test"

func setupCRUDTest(t *testing.T) (*server, func()) {
	t.Helper()

	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL is not set; skipping database integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := connectDB(ctx)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schemaforge_crud_test (
			id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			title TEXT NOT NULL,
			is_completed BOOLEAN NOT NULL DEFAULT FALSE
		)
	`)
	if err != nil {
		pool.Close()
		t.Fatalf("create test table: %v", err)
	}

	// Clear leftover rows from a previous interrupted test.
	_, err = pool.Exec(ctx, `TRUNCATE TABLE schemaforge_crud_test RESTART IDENTITY`)
	if err != nil {
		pool.Close()
		t.Fatalf("clear test table: %v", err)
	}

	s := &server{db: pool}

	cleanup := func() {
		defer pool.Close()
		_, err := pool.Exec(context.Background(),
			`DROP TABLE IF EXISTS schemaforge_crud_test`)
		if err != nil {
			t.Errorf("drop test table: %v", err)
		}
	}

	return s, cleanup
}

func TestCRUDRecords(t *testing.T) {
	s, cleanup := setupCRUDTest(t)
	defer cleanup()

	// POST: insert a record.
	payload := []byte(`{"title":"CRUD integration test","is_completed":false}`)
	req := httptest.NewRequest(
		http.MethodPost,
		"/records/"+crudTestTable,
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("POST: expected 201, got %d: %s", rec.Code, rec.Body.String())
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
	req = httptest.NewRequest(
		http.MethodGet, "/records/"+crudTestTable, nil,
	)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET: expected 200, got %d: %s", rec.Code, rec.Body.String())
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
	req = httptest.NewRequest(
		http.MethodPatch,
		fmt.Sprintf("/records/%s/%d", crudTestTable, recordID),
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var completed bool
	err := s.db.QueryRow(context.Background(),
		`SELECT is_completed FROM schemaforge_crud_test WHERE id = $1`,
		recordID,
	).Scan(&completed)
	if err != nil {
		t.Fatalf("verify PATCH: %v", err)
	}
	if !completed {
		t.Fatal("PATCH did not update is_completed to true")
	}

	// DELETE: remove the record.
	req = httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/records/%s/%d", crudTestTable, recordID),
		nil,
	)
	rec = httptest.NewRecorder()
	s.routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var count int
	err = s.db.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM schemaforge_crud_test WHERE id = $1`,
		recordID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("verify DELETE: %v", err)
	}
	if count != 0 {
		t.Fatalf("DELETE failed: record still exists")
	}
}

func TestRecordValidation(t *testing.T) {
	s, cleanup := setupCRUDTest(t)
	defer cleanup()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		want   int
	}{
		{
			name:   "PATCH invalid ID",
			method: http.MethodPatch,
			path:   "/records/" + crudTestTable + "/abc",
			body:   `{"title":"updated"}`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "DELETE invalid ID",
			method: http.MethodDelete,
			path:   "/records/" + crudTestTable + "/0",
			want:   http.StatusBadRequest,
		},
		{
			name:   "POST malformed JSON",
			method: http.MethodPost,
			path:   "/records/" + crudTestTable,
			body:   `{"title":`,
			want:   http.StatusBadRequest,
		},
		{
			name:   "PATCH unknown field",
			method: http.MethodPatch,
			path:   "/records/" + crudTestTable + "/1",
			body:   `{"unknown_field":"value"}`,
			want:   http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(
				tc.method,
				tc.path,
				bytes.NewBufferString(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			s.routes().ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Errorf("expected status %d, got %d; body: %s",
					tc.want, rec.Code, rec.Body.String())
			}
		})
	}
}
