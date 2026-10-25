package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type generateSchemaRequest struct {
	Description string `json:"description"`
}

var schemaIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func generateSchemaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Only POST is allowed",
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var req generateSchemaRequest
	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&req); err != nil {
		http.Error(w, `{"error":"Invalid JSON request"}`, http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Description) == "" {
		http.Error(w, `{"error":"description is required"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 95*time.Second)
	defer cancel()

	schema, err := generateSchema(ctx, req.Description)
	if err != nil {
		log.Printf("Schema generation failed: %v", err)
		http.Error(w, `{"error":"Schema generation failed"}`, http.StatusBadGateway)
		return
	}

	// Validate the table name.
	if !schemaIdentifierPattern.MatchString(schema.Table) {
		http.Error(w, `{"error":"Generated table name is invalid"}`, http.StatusUnprocessableEntity)
		return
	}

	// Allow only supported column types and valid, unique column names.
	allowedTypes := map[string]bool{
		"string":  true,
		"text":    true,
		"integer": true,
		"boolean": true,
	}

	seenColumns := make(map[string]bool)

	for _, column := range schema.Columns {
		if !schemaIdentifierPattern.MatchString(column.Name) {
			http.Error(w, `{"error":"Generated column name is invalid"}`, http.StatusUnprocessableEntity)
			return
		}

		if !allowedTypes[column.Type] {
			http.Error(w, `{"error":"Generated column type is unsupported"}`, http.StatusUnprocessableEntity)
			return
		}

		if seenColumns[column.Name] {
			http.Error(w, `{"error":"Generated column names must be unique"}`, http.StatusUnprocessableEntity)
			return
		}

		seenColumns[column.Name] = true
	}

	if len(schema.Columns) == 0 {
		http.Error(w, `{"error":"Generated schema has no columns"}`, http.StatusUnprocessableEntity)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(schema)
}
