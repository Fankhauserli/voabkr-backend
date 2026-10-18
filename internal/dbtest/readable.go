package dbtest

import (
	"context"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/jackc/pgx/v5/pgtype"
)

// ReadableDeck is one row as readable_decks returns it: the live Deck's columns
// and the Access role the caller reads it under.
type ReadableDeck struct {
	Deck       db.Deck
	AccessRole string
}

// ReadableDecks calls the readable_decks function directly, so the access
// contract test compares the SQL rule's rows and roles with the Go predicates
// rather than with a query that projects the function.
func ReadableDecks(t *testing.T, caller pgtype.Int8) []ReadableDeck {
	t.Helper()

	rows, err := pool(t).Query(context.Background(), `
		SELECT id, name, type, owner_id, is_public, share_token,
		       created_at, updated_at, deleted_at, access_role
		FROM readable_decks($1)`, caller)
	if err != nil {
		t.Fatalf("failed to call readable_decks: %v", err)
	}
	defer rows.Close()

	var decks []ReadableDeck
	for rows.Next() {
		var deck ReadableDeck
		if err := rows.Scan(
			&deck.Deck.ID,
			&deck.Deck.Name,
			&deck.Deck.Type,
			&deck.Deck.OwnerID,
			&deck.Deck.IsPublic,
			&deck.Deck.ShareToken,
			&deck.Deck.CreatedAt,
			&deck.Deck.UpdatedAt,
			&deck.Deck.DeletedAt,
			&deck.AccessRole,
		); err != nil {
			t.Fatalf("failed to scan a readable Deck: %v", err)
		}
		decks = append(decks, deck)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to read the readable Decks: %v", err)
	}
	return decks
}
