package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var embedMigrations embed.FS

// Run executes all pending database migrations against the given connection string.
func Run(dbConnString string) error {
	db, err := sql.Open("pgx", dbConnString)
	if err != nil {
		return fmt.Errorf("failed to open database connection for migrations: %w", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database for migrations: %w", err)
	}

	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	log.Println("Running database migrations...")
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	log.Println("Database migrations applied successfully")

	return nil
}

// UpSQL returns the Up section of the embedded migration with the given version.
// A test seeds rows in the state a migration is meant to repair, then applies
// this statement directly: the package database already has every migration
// applied before the test runs. The Up section must be a single statement,
// because the caller executes it in one call.
func UpSQL(version int64) (string, error) {
	pattern := fmt.Sprintf("%05d_*.sql", version)
	files, err := fs.Glob(embedMigrations, pattern)
	if err != nil {
		return "", fmt.Errorf("failed to find migration %d: %w", version, err)
	}
	if len(files) != 1 {
		return "", fmt.Errorf("migration %d: expected one file matching %q, found %d", version, pattern, len(files))
	}

	content, err := fs.ReadFile(embedMigrations, files[0])
	if err != nil {
		return "", fmt.Errorf("failed to read migration %d: %w", version, err)
	}

	_, up, found := strings.Cut(string(content), "-- +goose Up")
	if !found {
		return "", fmt.Errorf("migration %d has no Up section", version)
	}
	up, _, _ = strings.Cut(up, "-- +goose Down")

	return strings.TrimSpace(up), nil
}
