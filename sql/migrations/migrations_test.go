package migrations

import (
	"testing"

	"github.com/pressly/goose/v3"
)

func TestEmbeddedMigrations(t *testing.T) {
	goose.SetBaseFS(embedMigrations)

	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("failed to set goose dialect: %v", err)
	}

	migrations, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		t.Fatalf("failed to collect migrations: %v", err)
	}

	if len(migrations) == 0 {
		t.Fatalf("expected at least 1 migration, got 0")
	}

	t.Logf("Found %d migrations:", len(migrations))
	for _, m := range migrations {
		t.Logf("- %s (version: %d)", m.Source, m.Version)
		if m.Version != 1 {
			t.Errorf("expected version 1 for first migration, got %d", m.Version)
		}
	}
}
