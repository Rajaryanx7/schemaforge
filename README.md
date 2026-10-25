# SchemaForge

AI-powered PostgreSQL schema generation and REST API built with Go.

## Features

- AI schema generation using Google Gemini
- PostgreSQL SQL generation
- Schema execution with confirmation
- Dynamic CRUD REST API
- Input validation
- Automated tests

## Tech Stack

- Go
- PostgreSQL
- pgx
- Google Gemini API
- Docker

## Setup

1. Install Go, Docker, and obtain a Gemini API key.
2. Clone this repository.
3. Configure a `.env` file with `DATABASE_URL`, `PORT`, and `GEMINI_API_KEY`.
4. Install dependencies:

   `go mod download`

5. Start the server:

   `go run .`

The server runs at `http://localhost:3001`.

## API Endpoints

| Method | Endpoint | Purpose |
|---|---|---|
| GET | `/` | Welcome message |
| GET | `/health` | Health check |
| POST | `/generate-schema` | Generate schema with AI |
| POST | `/generate-sql` | Generate PostgreSQL SQL |
| POST | `/execute-sql` | Execute confirmed schema |
| POST | `/tables` | Create table |
| GET | `/tables` | List tables |
| POST | `/records/{table}` | Insert record |
| GET | `/records/{table}` | List records |
| PATCH | `/records/{table}/{id}` | Update record |
| DELETE | `/records/{table}/{id}` | Delete record |

## Testing

`go test ./...`

`go test -race ./...`

## Security

- Never commit `.env` or API keys.
- Review generated SQL before production use.
- Use appropriate database permissions.

## Status

Under active development.
