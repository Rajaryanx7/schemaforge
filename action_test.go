package main

import (
	"testing"
)

func TestDecodeAction(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid insert",
			input:   `{"action":"insert","table":"tasks","values":{"title":"Finish project"}}`,
			wantErr: false,
		},
		{
			name:    "valid update",
			input:   `{"action":"update","table":"tasks","id":5,"values":{"completed":true}}`,
			wantErr: false,
		},
		{
			name:    "valid delete",
			input:   `{"action":"delete","table":"tasks","id":5}`,
			wantErr: false,
		},
		{
			name:    "missing delete ID",
			input:   `{"action":"delete","table":"tasks"}`,
			wantErr: true,
		},
		{
			name:    "missing update values",
			input:   `{"action":"update","table":"tasks","id":5}`,
			wantErr: true,
		},
		{
			name:    "unsupported action",
			input:   `{"action":"drop","table":"tasks"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeAction([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Fatalf("decodeAction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
