package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type executeSchemaRequest struct {
	Schema  generatedSchema `json:"schema"`
	Confirm bool            `json:"confirm"`
}

func (s *server) executeSchemaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Only POST is allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var req executeSchemaRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid JSON request")
		return
	}

	// Require explicit approval before making a database change.
	if !req.Confirm {
		writeJSONError(w, http.StatusBadRequest, "Set confirm to true to approve table creation")
		return
	}

	schema := req.Schema

	// Validate the table and column identifiers.
	if !schemaIdentifierPattern.MatchString(schema.Table) {
		writeJSONError(w, http.StatusUnprocessableEntity, "Invalid table name")
		return
	}

	if len(schema.Columns) == 0 || len(schema.Columns) > 100 {
		writeJSONError(w, http.StatusUnprocessableEntity, "Schema must contain between 1 and 100 columns")
		return
	}

	sqlTypes := map[string]string{
		"string":  "TEXT",
		"text":    "TEXT",
		"integer": "INTEGER",
		"boolean": "BOOLEAN",
	}

	seen := make(map[string]bool)
	columns := []string{
		"id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
	}

	for _, column := range schema.Columns {
		if !schemaIdentifierPattern.MatchString(column.Name) {
			writeJSONError(w, http.StatusUnprocessableEntity, "Invalid column name")
			return
		}

		if column.Name == "id" {
			writeJSONError(w, http.StatusUnprocessableEntity, "The id column is managed automatically")
			return
		}

		if seen[column.Name] {
			writeJSONError(w, http.StatusUnprocessableEntity, "Column names must be unique")
			return
		}
		seen[column.Name] = true

		sqlType, ok := sqlTypes[strings.ToLower(column.Type)]
		if !ok {
			writeJSONError(w, http.StatusUnprocessableEntity, "Unsupported column type")
			return
		}

		columns = append(columns, column.Name+" "+sqlType)
	}

	// Quote the already-validated table and column identifiers.
	quotedColumns := make([]string, 0, len(columns))
	quotedColumns = append(quotedColumns,
		`"id" BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY`,
	)

	for _, column := range req.Schema.Columns {
		sqlType := sqlTypes[strings.ToLower(column.Type)]
		quotedColumns = append(quotedColumns, `"`+column.Name+`" `+sqlType)
	}

	ddl := `CREATE TABLE "` + schema.Table + `" (` +
		strings.Join(quotedColumns, ", ") + `)`

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Prevent duplicate creation; do not silently overwrite an existing table.
	tx, err := s.db.Begin(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not start database transaction")
		return
	}
	defer tx.Rollback(ctx)

	// Serialize SchemaForge table creation to reduce concurrent duplicate races.
	_, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734921)`)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not lock schema creation")
		return
	}

	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = current_schema()
			  AND table_name = $1
		)`,
		schema.Table,
	).Scan(&exists)

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "Could not check whether the table exists")
		return
	}

	if exists {
		writeJSONError(w, http.StatusConflict, "Table already exists")
		return
	}

	if _, err := tx.Exec(ctx, ddl); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "PostgreSQL could not create the table")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxCommitRollback) {
			writeJSONError(w, http.StatusInternalServerError, "Table creation transaction was rolled back")
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "Could not commit table creation")
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message": "Table created successfully",
		"table":   schema.Table,
	})
}
