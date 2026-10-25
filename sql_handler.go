package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func generateSQLHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "Only POST is allowed")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var schema generatedSchema
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&schema); err != nil {
		writeJSONError(w, http.StatusBadRequest, "Invalid schema JSON")
		return
	}

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
	columns := make([]string, 0, len(schema.Columns)+1)

	// SchemaForge manages the ID column itself.
	columns = append(columns, "    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY")

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

		sqlType, ok := sqlTypes[column.Type]
		if !ok {
			writeJSONError(w, http.StatusUnprocessableEntity, "Unsupported column type")
			return
		}

		columns = append(columns, fmt.Sprintf("    %s %s", column.Name, sqlType))
	}

	statement := fmt.Sprintf(
		"CREATE TABLE %s (\n%s\n);",
		schema.Table,
		strings.Join(columns, ",\n"),
	)

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"sql": statement,
	})
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
