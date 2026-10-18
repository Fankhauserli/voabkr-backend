package dbtest

import (
	"context"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/migrations"
)

// SetDeckDeletedAt stamps a Deck's soft-delete moment. A test uses it to put a
// row in a state no current writer produces — a deletion moment in the future,
// which every live-Deck read treats as deleted.
func SetDeckDeletedAt(t *testing.T, deckID int64, at time.Time) {
	t.Helper()

	if _, err := pool(t).Exec(context.Background(),
		`UPDATE decks SET deleted_at = $2 WHERE id = $1`, deckID, at); err != nil {
		t.Fatalf("failed to stamp Deck %d's deletion moment: %v", deckID, err)
	}
}

// ApplyMigration runs the Up statements of the embedded migration with the given
// version against this package's database. A test seeds rows in their
// pre-migration state, then applies the migration it is testing; the package
// database has every migration applied before any test runs.
func ApplyMigration(t *testing.T, version int64) {
	t.Helper()

	statement, err := migrations.UpSQL(version)
	if err != nil {
		t.Fatalf("failed to read migration %d: %v", version, err)
	}
	if _, err := pool(t).Exec(context.Background(), statement); err != nil {
		t.Fatalf("failed to apply migration %d: %v", version, err)
	}
}
