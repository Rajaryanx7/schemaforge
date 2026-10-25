# SchemaForge

AI-powered PostgreSQL schema generation and REST API built with Go. SchemaForge also provides a reusable Go library for validating natural-language database actions, previewing proposed changes, and executing them after explicit confirmation.

## Features

- AI-powered PostgreSQL schema generation using Google Gemini
- PostgreSQL SQL generation and execution
- Dynamic CRUD REST API
- Reusable Go library for database actions
- Action validation against database tables and columns
- Preview tokens with expiration and single-use confirmation
- Parameterized SQL execution
- Automated tests

## Tech Stack

- Go
- PostgreSQL
- pgx
- Google Gemini API
- Docker

## Requirements

- Go installed
- PostgreSQL database
- Google Gemini API key for AI-powered features

Docker is useful for running PostgreSQL locally.

## Setup

1. Clone the repository:

   ```bash
   git clone https://github.com/Rajaryanx7/schemaforge.git
   cd schemaforge
   ```

2. Create a `.env` file:

   ```env
   DATABASE_URL=postgres://USER:PASSWORD@localhost:5432/schemaforge
   PORT=3001
   GEMINI_API_KEY=your_gemini_api_key
   ```

   Replace the database credentials and API key with your own values.

3. Download dependencies:

   ```bash
   go mod download
   ```

4. Start the server:

   ```bash
   go run .
   ```

The server runs at `http://localhost:3001` when configured with port `3001`.

## HTTP API

| Method | Endpoint | Purpose |
|---|---|---|
| GET | `/` | Welcome message |
| GET | `/health` | Health check |
| POST | `/generate-schema` | Generate a schema with AI |
| POST | `/generate-sql` | Generate PostgreSQL SQL |
| POST | `/execute-sql` | Execute confirmed schema SQL |
| POST | `/tables` | Create a table |
| GET | `/tables` | List tables |
| POST | `/records/{table}` | Insert a record |
| GET | `/records/{table}` | List records |
| PATCH | `/records/{table}/{id}` | Update a record |
| DELETE | `/records/{table}/{id}` | Delete a record |
| POST | `/action` | Parse, validate, and preview a natural-language database action |
| POST | `/action/execute` | Execute a previewed action after confirmation |

### Natural-language action workflow

First, request a preview:

```bash
curl -X POST http://localhost:3001/action \
  -H "Content-Type: application/json" \
  -d '{
    "instruction": "In the task_demo table, mark record ID 3 as completed by setting is_completed to true"
  }'
```

The response includes the proposed action, a `preview_token`, and `requires_confirmation: true`. Previewing an action does not change the database.

After reviewing the action, submit the returned action and token to `/action/execute` with `"confirm": true`. The token is short-lived and can only be used once.

**Important:** Only confirm actions you understand and intend to execute.

## Go Library

The reusable package is located in `schemaforge/`.

### Initialize the client

```go
package main

import (
    "log"
    "os"

    "schemaforge/schemaforge"
)

func main() {
    client, err := schemaforge.New(schemaforge.Config{
        DatabaseURL:  os.Getenv("DATABASE_URL"),
        GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
    })
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    // Use client.Preview() and client.Execute() here.
}
```

Set `DATABASE_URL` and `GEMINI_API_KEY` in your environment before running the application.

### Preview an action

```go
action, token, err := client.Preview(
    ctx,
    "In the task_demo table, mark record ID 3 as completed by setting is_completed to true",
)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Proposed action: %+v\n", *action)
fmt.Println("Preview token:", token)
```

`Preview()` parses the instruction, validates the proposed action against the database schema, and returns a confirmation token. It does not execute the database change.

### Execute after confirmation

```go
result, err := client.Execute(ctx, *action, token, true)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Result: %+v\n", result)
```

Only call `Execute()` after reviewing the proposed action and obtaining explicit user confirmation. The token must match the previewed action, must not be expired, and cannot be reused.

For a complete example, see [`examples/basic/main.go`](examples/basic/main.go).

## Testing

Run the test suite:

```bash
go test ./...
```

Run tests with the race detector:

```bash
go test -race ./...
```

Run static analysis:

```bash
go vet ./...
```

## Security

- Never commit `.env` files or API keys.
- Review generated actions and SQL before execution.
- Use least-privilege PostgreSQL credentials.
- Keep confirmation tokens private and short-lived.
- Treat AI-generated actions as untrusted until validated and reviewed.
- Add authentication and authorization before exposing database-changing endpoints publicly.

## Project Status

Under active development.
