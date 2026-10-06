package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"ehr-backend/internal/database"
	"ehr-backend/internal/server"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if len(jwtSecret) < 32 || jwtSecret == "YOUR_RANDOM_SECRET" {
		log.Fatal("JWT_SECRET is required and must be at least 32 characters (for example: openssl rand -hex 32)")
	}

	ctx := context.Background()

	db, err := database.Connect(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	log.Println("Connected to PostgreSQL")

	handler, _ := server.New(db, jwtSecret)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute, // PDF / CSV generation
		IdleTimeout:       2 * time.Minute,
	}

	log.Println("EHR API running on http://localhost:8080")

	if err := srv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
