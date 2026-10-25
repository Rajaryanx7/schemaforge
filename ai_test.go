package main

import (
	"context"
	"os"
	"testing"
)

func TestGenerateSchema(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY is not exported; skipping live API test")
	}

	result, err := generateSchema(
		context.Background(),
		"Create a task manager with a title, description, and completion status",
	)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Generated schema: %+v", result)

	if result.Table == "" || len(result.Columns) == 0 {
		t.Fatal("AI returned an incomplete schema")
	}
}
