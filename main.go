package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"schemaforge/schemaforge"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

const (
	appVersion   = "0.1.0"
	defaultPort  = "3001"
	dbTimeout    = 5 * time.Second
	maxBodyBytes = 1 << 20
	maxColumns   = 100
)

var (
	identPattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

	allowedTypes = map[string]string{
		"string":  "VARCHAR(255)",
		"text":    "TEXT",
		"integer": "INTEGER",
		"boolean": "BOOLEAN",
	}

	systemColumns = map[string]bool{
		"ctid": true, "xmin": true, "xmax": true,
		"cmin": true, "cmax": true, "tableoid": true,
	}
)

type server struct {
	db      *pgxpool.Pool
	library *schemaforge.Client
}

type apiError struct {
	Status  int
	Message string
}

type column struct {
	Name     string
	DataType string
	Identity bool
}

type createTableRequest struct {
	Name    string            `json:"name"`
	Columns map[string]string `json:"columns"`
}

type columnSpec struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type createTableResponse struct {
	Message string       `json:"message"`
	Table   string       `json:"table"`
	Columns []columnSpec `json:"columns"`
}

type listTablesResponse struct {
	Tables []string `json:"tables"`
	Count  int      `json:"count"`
}

type insertResponse struct {
	Message string         `json:"message"`
	Table   string         `json:"table"`
	Record  map[string]any `json:"record"`
}

type healthResponse struct {
	Status   string `json:"status"`
	Server   string `json:"server"`
	Database string `json:"database"`
}

// -----------------------------------------------------------------------------
// JSON and HTTP helpers
// -----------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("JSON encoding error: %v", err)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("{\"error\":\"internal server error\"}\n"))
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeAPIError(w http.ResponseWriter, e *apiError) {
	writeError(w, e.Status, e.Message)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, strict bool) *apiError {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &apiError{
			http.StatusUnsupportedMediaType,
			"Content-Type must be application/json",
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()

	if strict {
		dec.DisallowUnknownFields()
	}

	if err := dec.Decode(dst); err != nil {
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		var maxErr *http.MaxBytesError

		switch {
		case errors.Is(err, io.EOF):
			return &apiError{http.StatusBadRequest, "request body must not be empty"}
		case errors.As(err, &maxErr):
			return &apiError{http.StatusRequestEntityTooLarge, "request body is too large"}
		case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
			return &apiError{http.StatusBadRequest, "request body contains malformed JSON"}
		case errors.As(err, &typeErr):
			if typeErr.Field == "" {
				return &apiError{http.StatusBadRequest, "expected a JSON object"}
			}
			return &apiError{
				http.StatusBadRequest,
				fmt.Sprintf("field %q has the wrong JSON type", typeErr.Field),
			}
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return &apiError{http.StatusBadRequest, err.Error()}
		default:
			return &apiError{http.StatusBadRequest, "invalid request body"}
		}
	}

	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &apiError{
			http.StatusBadRequest,
			"request body must contain a single JSON value",
		}
	}

	return nil
}

// -----------------------------------------------------------------------------
// Database error handling
// -----------------------------------------------------------------------------

func handleDBError(w http.ResponseWriter, op string, err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		log.Printf("database timeout during %s: %v", op, err)
		writeError(w, http.StatusServiceUnavailable, "the database did not respond in time")
		return
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		log.Printf(
			"PostgreSQL error during %s: code=%s message=%q detail=%q",
			op, pgErr.Code, pgErr.Message, pgErr.Detail,
		)

		switch pgErr.Code {
		case "42P07", "42710":
			writeError(w, http.StatusConflict, "a table with that name already exists")
		case "42P01":
			writeError(w, http.StatusNotFound, "table does not exist")
		case "42701":
			writeError(w, http.StatusBadRequest, "duplicate column name")
		case "23505":
			writeError(w, http.StatusConflict, "a record with the same unique value already exists")
		case "23502":
			writeError(w, http.StatusBadRequest, "a required column is missing or null")
		case "22001":
			writeError(w, http.StatusBadRequest, "a text value is too long for its column")
		case "22003":
			writeError(w, http.StatusBadRequest, "a numeric value is out of range")
		case "22P02", "22021", "22007", "22008":
			writeError(w, http.StatusBadRequest, "a value has an invalid format")
		default:
			if strings.HasPrefix(pgErr.Code, "23") {
				writeError(w, http.StatusBadRequest, "the record violates a database constraint")
				return
			}
			writeError(w, http.StatusInternalServerError, "internal server error")
		}
		return
	}

	log.Printf("unexpected error during %s: %v", op, err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}

// -----------------------------------------------------------------------------
// Validation and SQL helpers
// -----------------------------------------------------------------------------

func validateIdentifier(kind, name string) error {
	if !identPattern.MatchString(name) {
		return fmt.Errorf(
			"invalid %s %q: use 1-63 lowercase letters, digits or underscores, not starting with a digit",
			kind, name,
		)
	}
	return nil
}

func validateTableName(name string) error {
	if err := validateIdentifier("table name", name); err != nil {
		return err
	}

	if strings.HasPrefix(name, "pg_") {
		return fmt.Errorf("invalid table name %q: the pg_ prefix is reserved", name)
	}

	return nil
}

func validateColumnName(name string) error {
	if name == "id" {
		return errors.New(`column "id" is generated automatically and cannot be defined`)
	}

	if err := validateIdentifier("column name", name); err != nil {
		return err
	}

	if systemColumns[name] {
		return fmt.Errorf("invalid column name %q: reserved by PostgreSQL", name)
	}

	return nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func qualifiedTable(table string) string {
	return quoteIdent("public") + "." + quoteIdent(table)
}

func convertValue(col column, v any) (any, error) {
	if v == nil {
		return nil, nil
	}

	switch col.DataType {
	case "character varying", "character", "text":
		s, ok := v.(string)
		if !ok {
			return nil, errors.New("must be a string")
		}
		if strings.ContainsRune(s, 0) {
			return nil, errors.New("must not contain NUL characters")
		}
		return s, nil

	case "smallint", "integer", "bigint":
		n, ok := v.(json.Number)
		if !ok {
			return nil, errors.New("must be an integer")
		}

		bits := 64
		switch col.DataType {
		case "smallint":
			bits = 16
		case "integer":
			bits = 32
		}

		i, err := strconv.ParseInt(n.String(), 10, bits)
		if err != nil {
			return nil, errors.New("must be a whole number within the column's range")
		}
		return i, nil

	case "boolean":
		b, ok := v.(bool)
		if !ok {
			return nil, errors.New("must be true or false")
		}
		return b, nil

	default:
		return nil, fmt.Errorf("columns of type %q are not supported", col.DataType)
	}
}

// -----------------------------------------------------------------------------
// Database helpers
// -----------------------------------------------------------------------------

func (s *server) loadColumns(ctx context.Context, table string) ([]column, error) {
	const query = `
		SELECT c.column_name::text, c.data_type::text, (c.is_identity = 'YES')
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema = 'public'
		  AND t.table_type = 'BASE TABLE'
		  AND c.table_name::text = $1
		ORDER BY c.ordinal_position`

	rows, err := s.db.Query(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []column

	for rows.Next() {
		var c column
		if err := rows.Scan(&c.Name, &c.DataType, &c.Identity); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}

	return cols, rows.Err()
}

func (s *server) loadTable(
	ctx context.Context,
	w http.ResponseWriter,
	table string,
) ([]column, bool) {
	if err := validateTableName(table); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}

	cols, err := s.loadColumns(ctx, table)
	if err != nil {
		handleDBError(w, "load columns", err)
		return nil, false
	}

	if len(cols) == 0 {
		writeError(w, http.StatusNotFound, fmt.Sprintf("table %q does not exist", table))
		return nil, false
	}

	return cols, true
}

// -----------------------------------------------------------------------------
// API handlers
// -----------------------------------------------------------------------------

func (s *server) handleRoot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"name":    "SchemaForge",
		"message": "Welcome to SchemaForge - define tables and get REST APIs automatically.",
		"version": appVersion,
	})
}

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		log.Printf("health check: database ping failed: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{
			Status: "degraded", Server: "running", Database: "unreachable",
		})
		return
	}

	writeJSON(w, http.StatusOK, healthResponse{
		Status: "ok", Server: "running", Database: "connected",
	})
}

func (s *server) handleCreateTable(w http.ResponseWriter, r *http.Request) {
	var req createTableRequest

	if apiErr := decodeJSON(w, r, &req, true); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	if err := validateTableName(req.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if len(req.Columns) == 0 {
		writeError(w, http.StatusBadRequest, "at least one column is required")
		return
	}

	if len(req.Columns) > maxColumns {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("too many columns (max %d)", maxColumns))
		return
	}

	names := make([]string, 0, len(req.Columns))
	for name := range req.Columns {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := []string{
		quoteIdent("id") + " BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
	}
	specs := []columnSpec{{Name: "id", Type: "bigint"}}

	for _, name := range names {
		if err := validateColumnName(name); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		clientType := strings.ToLower(strings.TrimSpace(req.Columns[name]))
		sqlType, ok := allowedTypes[clientType]
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"unsupported type %q for column %q (allowed: string, text, integer, boolean)",
				req.Columns[name], name,
			))
			return
		}

		defs = append(defs, quoteIdent(name)+" "+sqlType)
		specs = append(specs, columnSpec{Name: name, Type: clientType})
	}

	ddl := fmt.Sprintf(
		"CREATE TABLE %s (%s)",
		qualifiedTable(req.Name),
		strings.Join(defs, ", "),
	)

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	if _, err := s.db.Exec(ctx, ddl); err != nil {
		handleDBError(w, "create table", err)
		return
	}

	writeJSON(w, http.StatusCreated, createTableResponse{
		Message: "table created successfully",
		Table:   req.Name,
		Columns: specs,
	})
}

func (s *server) handleListTables(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	const query = `
		SELECT table_name::text
		FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
		ORDER BY table_name`

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		handleDBError(w, "list tables", err)
		return
	}
	defer rows.Close()

	tables := []string{}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			handleDBError(w, "scan table name", err)
			return
		}
		tables = append(tables, name)
	}

	if err := rows.Err(); err != nil {
		handleDBError(w, "iterate tables", err)
		return
	}

	writeJSON(w, http.StatusOK, listTablesResponse{
		Tables: tables,
		Count:  len(tables),
	})
}

func (s *server) handleInsertRecord(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	cols, ok := s.loadTable(ctx, w, table)
	if !ok {
		return
	}

	var body map[string]any
	if apiErr := decodeJSON(w, r, &body, false); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	if body == nil {
		writeError(w, http.StatusBadRequest, "request body must be a JSON object")
		return
	}

	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "request body must contain at least one field")
		return
	}

	byName := make(map[string]column, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
	}

	fields := make([]string, 0, len(body))
	for name := range body {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	quotedCols := make([]string, 0, len(fields))
	placeholders := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields))

	for i, name := range fields {
		col, exists := byName[name]
		if !exists {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"unknown field %q for table %q", name, table,
			))
			return
		}

		if col.Identity {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"field %q is generated automatically and cannot be set", name,
			))
			return
		}

		val, err := convertValue(col, body[name])
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"invalid value for field %q: %v", name, err,
			))
			return
		}

		quotedCols = append(quotedCols, quoteIdent(name))
		placeholders = append(placeholders, "$"+strconv.Itoa(i+1))
		args = append(args, val)
	}

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) RETURNING *",
		qualifiedTable(table),
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		handleDBError(w, "insert record", err)
		return
	}

	record, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if err != nil {
		handleDBError(w, "insert record (read result)", err)
		return
	}

	writeJSON(w, http.StatusCreated, insertResponse{
		Message: "record inserted successfully",
		Table:   table,
		Record:  record,
	})
}

func (s *server) handleListRecords(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	cols, ok := s.loadTable(ctx, w, table)
	if !ok {
		return
	}

	query := "SELECT * FROM " + qualifiedTable(table)

	for _, c := range cols {
		if c.Name == "id" {
			query += " ORDER BY " + quoteIdent("id")
			break
		}
	}

	rows, err := s.db.Query(ctx, query)
	if err != nil {
		handleDBError(w, "list records", err)
		return
	}

	records, err := pgx.CollectRows(rows, pgx.RowToMap)
	if err != nil {
		handleDBError(w, "list records (read rows)", err)
		return
	}

	if records == nil {
		records = []map[string]any{}
	}

	writeJSON(w, http.StatusOK, records)
}

func (s *server) handlePatchRecord(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	idText := r.PathValue("id")

	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	cols, ok := s.loadTable(ctx, w, table)
	if !ok {
		return
	}

	var hasID bool
	byName := make(map[string]column, len(cols))
	for _, c := range cols {
		byName[c.Name] = c
		if c.Name == "id" {
			hasID = true
		}
	}

	if !hasID {
		writeError(w, http.StatusBadRequest, "table does not have an id column")
		return
	}

	var body map[string]any
	if apiErr := decodeJSON(w, r, &body, false); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}

	if len(body) == 0 {
		writeError(w, http.StatusBadRequest, "request body must contain at least one field")
		return
	}

	fields := make([]string, 0, len(body))
	for name := range body {
		fields = append(fields, name)
	}
	sort.Strings(fields)

	sets := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields))

	for _, name := range fields {
		col, exists := byName[name]
		if !exists {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"unknown field %q for table %q", name, table,
			))
			return
		}

		if name == "id" || col.Identity {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"field %q cannot be updated", name,
			))
			return
		}

		val, err := convertValue(col, body[name])
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf(
				"invalid value for field %q: %v", name, err,
			))
			return
		}

		args = append(args, val)
		sets = append(sets, quoteIdent(name)+" = $"+strconv.Itoa(len(args)))
	}

	args = append(args, id)

	query := fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = $%d RETURNING *",
		qualifiedTable(table),
		strings.Join(sets, ", "),
		quoteIdent("id"),
		len(args),
	)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		handleDBError(w, "update record", err)
		return
	}

	record, err := pgx.CollectOneRow(rows, pgx.RowToMap)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "record not found")
		return
	}
	if err != nil {
		handleDBError(w, "update record (read result)", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "record updated successfully",
		"table":   table,
		"record":  record,
	})
}

func (s *server) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	table := r.PathValue("table")
	idText := r.PathValue("id")

	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
	defer cancel()

	cols, ok := s.loadTable(ctx, w, table)
	if !ok {
		return
	}

	hasID := false
	for _, col := range cols {
		if col.Name == "id" {
			hasID = true
			break
		}
	}

	if !hasID {
		writeError(w, http.StatusBadRequest, "table does not have an id column")
		return
	}

	query := fmt.Sprintf(
		"DELETE FROM %s WHERE %s = $1 RETURNING %s",
		qualifiedTable(table),
		quoteIdent("id"),
		quoteIdent("id"),
	)

	var deletedID int64
	err = s.db.QueryRow(ctx, query, id).Scan(&deletedID)

	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "record not found")
		return
	}

	if err != nil {
		handleDBError(w, "delete record", err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": "record deleted successfully",
		"table":   table,
		"id":      deletedID,
	})
}

func handleNotFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, "no such route (check the path and HTTP method)")
}

// -----------------------------------------------------------------------------
// Routing and middleware
// -----------------------------------------------------------------------------

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleRoot)
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("POST /generate-schema", generateSchemaHandler)
	mux.HandleFunc("POST /execute-sql", s.executeSchemaHandler)
	mux.HandleFunc("POST /generate-sql", generateSQLHandler)
	mux.HandleFunc("POST /tables", s.handleCreateTable)
	mux.HandleFunc("GET /tables", s.handleListTables)
	mux.HandleFunc("POST /records/{table}", s.handleInsertRecord)
	mux.HandleFunc("GET /records/{table}", s.handleListRecords)
	mux.HandleFunc("PATCH /records/{table}/{id}", s.handlePatchRecord)
	mux.HandleFunc("DELETE /records/{table}/{id}", s.handleDeleteRecord)
	mux.HandleFunc("POST /action", s.handleAction)
	mux.HandleFunc("POST /action/execute", s.handleExecuteAction)
	mux.HandleFunc("/", handleNotFound)

	return withMiddleware(mux)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		rec.Header().Set("X-Content-Type-Options", "nosniff")

		defer func() {
			if p := recover(); p != nil {
				log.Printf("panic while handling %s %s: %v", r.Method, r.URL.Path, p)
				writeError(rec, http.StatusInternalServerError, "internal server error")
			}

			log.Printf("%s %s -> %d (%s)",
				r.Method, r.URL.Path, rec.status,
				time.Since(start).Round(time.Microsecond),
			)
		}()

		next.ServeHTTP(rec, r)
	})
}

// -----------------------------------------------------------------------------
// Startup
// -----------------------------------------------------------------------------

func connectDB(ctx context.Context) (*pgxpool.Pool, error) {
	url := os.Getenv("DATABASE_URL")
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("DATABASE_URL is not set; add it to .env or export it in your shell")
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}

func run() error {
	// Load .env. Variables already exported in the shell take precedence.
	if err := godotenv.Load(); err != nil {
		if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
			return fmt.Errorf(
				"could not load .env and DATABASE_URL is not set: %w", err,
			)
		}
		log.Println("No .env file found; using DATABASE_URL from the environment")
	}

	ctx, stop := signal.NotifyContext(
		context.Background(), os.Interrupt, syscall.SIGTERM,
	)
	defer stop()

	pool, err := connectDB(ctx)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	library, err := schemaforge.NewWithPool(
		pool,
		os.Getenv("GEMINI_API_KEY"),
	)
	if err != nil {
		return fmt.Errorf("initialize SchemaForge library: %w", err)
	}
	defer library.Close()

	s := &server{
		db:      pool,
		library: library,
	}

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           s.routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)

	go func() {
		log.Printf("SchemaForge %s listening on http://localhost:%s", appVersion, port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil

	case <-ctx.Done():
		log.Println("shutting down...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return srv.Shutdown(shutdownCtx)
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}
