package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// systemDeckNames are the five starter Decks production carries. Their rows are
// ownerless and public, and the seeding stamped a deletion moment far in the
// future, so every live-Deck read treats them as gone.
var systemDeckNames = []string{
	"2000 Essential Korean Words: Beginner",
	"2000 Essential Korean Words: Intermediate",
	"2000 Essential Korean Words: Advanced",
	"Korean Grammar in Use: Beginning",
	"Korean Grammar in Use: Intermediate",
}

// The restore migration clears the far-future deletion stamp on the System decks
// and on nothing else: the anonymous listing, a by-id read and a Card listing all
// answer for them, while a learner-owned deletion and a deletion that has already
// happened stay gone.
func TestRestoreSystemDecksMigration(t *testing.T) {
	h := newTestHandler(t)
	queries := dbtest.Queries(t)
	ctx := t.Context()

	owner := dbtest.SeedUser(t, "owner@example.com")

	farFuture := time.Now().Add(100 * 365 * 24 * time.Hour)
	systemDeckIDs := make([]int64, 0, len(systemDeckNames))
	for _, name := range systemDeckNames {
		id := dbtest.SeedNamedDeck(t, 0, true, name)
		dbtest.SeedCard(t, id, "나무", "tree")
		dbtest.SetDeckDeletedAt(t, id, farFuture)
		systemDeckIDs = append(systemDeckIDs, id)
	}

	// A learner's deleted Deck is not restored, even with a deletion moment that
	// has not arrived: the migration belongs to ownerless Decks alone.
	ownedDeleted := dbtest.SeedNamedDeck(t, owner, false, "Learner Deck")
	if err := queries.DeleteDeck(ctx, ownedDeleted); err != nil {
		t.Fatalf("failed to delete the learner's Deck: %v", err)
	}
	ownedFutureDeleted := dbtest.SeedNamedDeck(t, owner, false, "Learner Future Deck")
	dbtest.SetDeckDeletedAt(t, ownedFutureDeleted, farFuture)

	// A System deck whose deletion has already happened stays deleted.
	historicSystem := dbtest.SeedNamedDeck(t, 0, true, "Historic System Deck")
	dbtest.SetDeckDeletedAt(t, historicSystem, time.Now().Add(-24*time.Hour))

	dbtest.ApplyMigration(t, 6)
	// The migration is idempotent: a second run leaves every row as the first did.
	dbtest.ApplyMigration(t, 6)

	listed := anonymousDecks(t, h)
	byName := make(map[string]*types.Deck, len(listed))
	for i := range listed {
		byName[listed[i].Name] = &listed[i]
	}

	for _, name := range systemDeckNames {
		deck, ok := byName[name]
		if !ok {
			t.Fatalf("expected %q in the anonymous listing, got %v", name, deckNamesOf(listed))
		}
		if deck.OwnerId != 0 {
			t.Fatalf("expected %q to stay ownerless, got ownerId %d", name, deck.OwnerId)
		}
		if !deck.IsPublic || deck.UserAccessRole != access.RolePublic {
			t.Fatalf("expected %q to read public, got isPublic=%t role=%q", name, deck.IsPublic, deck.UserAccessRole)
		}
	}

	for _, name := range []string{"Learner Deck", "Learner Future Deck", "Historic System Deck"} {
		if _, ok := byName[name]; ok {
			t.Fatalf("expected the deleted Deck %q to stay out of the listing, got %v", name, deckNamesOf(listed))
		}
	}

	for i, deckID := range systemDeckIDs {
		if w := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(deckID)); w.Code != http.StatusOK {
			t.Fatalf("expected the restored Deck %q to read, got %d: %s", systemDeckNames[i], w.Code, w.Body.String())
		}

		w := get(t, h.GetCards, access.Anonymous, cardsRoute, cardsPath(deckID))
		if w.Code != http.StatusOK {
			t.Fatalf("expected the restored Deck's Cards to list, got %d: %s", w.Code, w.Body.String())
		}
		var cards []types.Card
		if err := json.Unmarshal(w.Body.Bytes(), &cards); err != nil {
			t.Fatalf("failed to parse the Cards: %v", err)
		}
		if len(cards) != 1 || cards[0].KoreanWord != "나무" {
			t.Fatalf("expected the restored Deck's Card, got %v", cards)
		}
	}

	for _, deckID := range []int64{ownedDeleted, ownedFutureDeleted, historicSystem} {
		if w := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(deckID)); w.Code != http.StatusNotFound {
			t.Fatalf("expected Deck %d to stay unreadable, got %d: %s", deckID, w.Code, w.Body.String())
		}
		if w := get(t, h.GetCards, access.Anonymous, cardsRoute, cardsPath(deckID)); w.Code != http.StatusNotFound {
			t.Fatalf("expected Deck %d's Cards to stay unreadable, got %d: %s", deckID, w.Code, w.Body.String())
		}
	}
}

// anonymousDecks reads the listing an anonymous caller sees.
func anonymousDecks(t *testing.T, h *handlers.Handler) []types.Deck {
	t.Helper()

	w := get(t, h.GetDecks, access.Anonymous, decksRoute, decksRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the anonymous listing: %d: %s", w.Code, w.Body.String())
	}

	var decks []types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &decks); err != nil {
		t.Fatalf("failed to parse the anonymous listing: %v", err)
	}
	return decks
}

// deckNamesOf returns the names a listing carries, for a failure message.
func deckNamesOf(decks []types.Deck) []string {
	names := make([]string, 0, len(decks))
	for i := range decks {
		names = append(names, decks[i].Name)
	}
	return names
}
