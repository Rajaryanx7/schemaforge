package schemaforge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseInstruction(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantErr    string
		wantAction string
	}{
		{
			name:       "valid insert",
			status:     http.StatusOK,
			body:       `{"candidates":[{"content":{"parts":[{"text":"{\"action\":\"insert\",\"table\":\"students\",\"values\":{\"name\":\"Raj\"}}"}]}}]}`,
			wantAction: "insert",
		},
		{
			name:    "HTTP error",
			status:  http.StatusServiceUnavailable,
			body:    `{"error":"unavailable"}`,
			wantErr: "Gemini returned HTTP 503",
		},
		{
			name:    "invalid response JSON",
			status:  http.StatusOK,
			body:    `{`,
			wantErr: "unexpected end of JSON input",
		},
		{
			name:    "no candidates",
			status:  http.StatusOK,
			body:    `{"candidates":[]}`,
			wantErr: "Gemini returned no action",
		},
		{
			name:    "no parts",
			status:  http.StatusOK,
			body:    `{"candidates":[{"content":{"parts":[]}}]}`,
			wantErr: "Gemini returned no action",
		},
		{
			name:    "invalid action JSON",
			status:  http.StatusOK,
			body:    `{"candidates":[{"content":{"parts":[{"text":"not json"}]}}]}`,
			wantErr: "invalid action JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("missing JSON content type")
				}
				if r.Header.Get("x-goog-api-key") != "test-key" {
					t.Errorf("API key header not set")
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("invalid request body: %v", err)
				}

				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			oldEndpoint := geminiEndpoint
			oldClient := geminiHTTPClient
			geminiEndpoint = server.URL
			geminiHTTPClient = server.Client()
			defer func() {
				geminiEndpoint = oldEndpoint
				geminiHTTPClient = oldClient
			}()

			client := &Client{geminiKey: "test-key"}
			action, err := client.parseInstruction(context.Background(), "add a student")

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action.Type != ActionType(tt.wantAction) {
				t.Errorf("action type = %q, want %q", action.Type, tt.wantAction)
			}
			if action.Table != "students" {
				t.Errorf("table = %q, want students", action.Table)
			}
		})
	}
}
