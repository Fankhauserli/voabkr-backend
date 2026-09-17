package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/sql/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func initDB() (*db.Queries, error) {
	dbConnString := os.Getenv("DB_CONN_STRING")
	if dbConnString == "" {
		return nil, fmt.Errorf("DB_CONN_STRING environment variable is not set")
	}

	// Run pending database migrations to initialize/upgrade database schema
	if err := migrations.Run(dbConnString); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	pool, err := pgxpool.New(context.Background(), dbConnString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database pool: %w", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("Connected to database successfully")
	return db.New(pool), nil
}

func getDB() (*db.Queries, error) {
	return initDB()
}
