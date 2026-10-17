package cards_test

import (
	"context"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/cards"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
)

// TestMain provisions this package's Postgres with the migrations applied once,
// per ADR-0003. The harness records a missing container runtime and skips the
// database-backed tests locally; CI fails instead.
func TestMain(m *testing.M) {
	dbtest.Main(m)
}

// A Deck's Cards stop answering at the module's own interface once the Deck is
// deleted, not only through the handler that gates the route.
func TestByDeckOmitsTheCardsOfADeletedDeck(t *testing.T) {
	queries := dbtest.Setup(t)
	reader := cards.NewReader(queries, nil)
	ctx := context.Background()

	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "지운", "deleted")

	got, err := reader.ByDeck(ctx, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the Deck's Cards: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the Deck's Card to begin with, got %d", len(got))
	}

	if err := queries.DeleteDeck(ctx, deckID); err != nil {
		t.Fatalf("failed to delete the Deck: %v", err)
	}

	got, err = reader.ByDeck(ctx, deckID, cards.Page{})
	if err != nil {
		t.Fatalf("failed to read the deleted Deck's Cards: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected the deleted Deck's Cards to leave the listing, got %d", len(got))
	}
}
