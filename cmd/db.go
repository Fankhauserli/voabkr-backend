package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5"
)

func getDB() (*db.Queries, error) {
	dbConnString := os.Getenv("DB_CONN_STRING")
	if dbConnString == "" {
		return nil, fmt.Errorf("DB_CONN_STRING environment variable is not set")
	}

	conn, err := pgx.Connect(context.Background(), dbConnString)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	db := db.New(conn)

	return db, nil
}
