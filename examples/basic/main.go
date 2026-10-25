package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"schemaforge/schemaforge"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	client, err := schemaforge.New(schemaforge.Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		GeminiAPIKey: os.Getenv("GEMINI_API_KEY"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	ctx := context.Background()

	action, token, err := client.Preview(
		ctx,
		"In the task_demo table, mark record ID 3 as completed by setting is_completed to true",
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Proposed action: %+v\n", *action)
	fmt.Printf("Preview token: %s\n", token)
	fmt.Println("Preview only. No database changes were made.")
	fmt.Println("Execution remains disabled in this example.")
}
