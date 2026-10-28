package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// requireDBPassword reads the database password from the environment.
func requireDBPassword() (string, error) {
	pw := os.Getenv("SUPABASE_DB_PASSWORD")
	if pw == "" {
		return "", errors.New("SUPABASE_DB_PASSWORD is not set")
	}
	return pw, nil
}

func main() {
	dbPassword, err := requireDBPassword()
	if err != nil {
		log.Fatal(err)
	}
	encodedPassword := url.QueryEscape(dbPassword)

	// Direct connection for migration execution (IPv6/IPv4 direct or pooler)
	connStr := fmt.Sprintf("postgres://postgres:%s@db.dxbbbhwqokvfsedoiths.supabase.co:5432/postgres?sslmode=require", encodedPassword)
	if envURL := os.Getenv("DIRECT_URL"); envURL != "" {
		connStr = envURL
	} else if envURL := os.Getenv("DATABASE_URL"); envURL != "" {
		connStr = envURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		log.Fatalf("failed to parse connection config: %v", err)
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("unable to connect to Supabase: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("ping failed: %v", err)
	}
	fmt.Println("Successfully connected to Supabase PostgreSQL!")

	// Execute Schema DDL
	ddlPath := "internal/infrastructure/persistence/migrations/002_supabase_init.sql"
	ddlContent, err := os.ReadFile(ddlPath)
	if err != nil {
		// Fallback to 001_init.sql if 002 is not located relative to cwd
		ddlContent, err = os.ReadFile("internal/infrastructure/persistence/migrations/001_init.sql")
		if err != nil {
			log.Fatalf("failed to read migration file: %v", err)
		}
	}

	_, err = pool.Exec(ctx, string(ddlContent))
	if err != nil {
		log.Fatalf("failed to execute migration DDL: %v", err)
	}
	fmt.Println("Applied migration DDL successfully.")

	// Verify Account Seed Counts
	var count int
	err = pool.QueryRow(ctx, "SELECT count(*) FROM accounts;").Scan(&count)
	if err != nil {
		log.Fatalf("failed to query accounts count: %v", err)
	}
	fmt.Printf("Verification: %d accounts present in Supabase.\n", count)
}
