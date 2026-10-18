package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
)

// deckCounts is the part of a Deck body these tests read: how many Cards the
// Deck holds, and how many of the caller's Reviews of them are Due now.
type deckCounts struct {
	Id        int64 `json:"id"`
	CardCount int64 `json:"cardCount"`
	DueCount  int64 `json:"dueCount"`
}

// listingDeck returns the Deck the caller's listing carries for deckID.
func listingDeck(t *testing.T, h *handlers.Handler, caller, deckID int64) deckCounts {
	t.Helper()

	w := get(t, h.GetDecks, caller, decksRoute, decksRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the listing as %d: %d: %s", caller, w.Code, w.Body.String())
	}

	var decks []deckCounts
	if err := json.Unmarshal(w.Body.Bytes(), &decks); err != nil {
		t.Fatalf("failed to parse the listing: %v", err)
	}
	for _, deck := range decks {
		if deck.Id == deckID {
			return deck
		}
	}
	t.Fatalf("expected Deck %d in the listing, got %v", deckID, decks)
	return deckCounts{}
}

// singleDeck returns the Deck body the caller reads by id, with its counts.
func singleDeck(t *testing.T, h *handlers.Handler, caller, deckID int64) deckCounts {
	t.Helper()

	w := get(t, h.GetDeckByID, caller, deckRoute, deckPath(deckID))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read Deck %d as %d: %d: %s", deckID, caller, w.Code, w.Body.String())
	}

	var deck deckCounts
	if err := json.Unmarshal(w.Body.Bytes(), &deck); err != nil {
		t.Fatalf("failed to parse the Deck: %v", err)
	}
	return deck
}

// sharedDeck returns the Deck body the share link serves, with its counts.
func sharedDeck(t *testing.T, h *handlers.Handler, token string) deckCounts {
	t.Helper()

	w := get(t, h.GetSharedDeckByToken, access.Anonymous, sharedDeckRoute, sharedDeckPath(token))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the shared Deck: %d: %s", w.Code, w.Body.String())
	}

	var deck deckCounts
	if err := json.Unmarshal(w.Body.Bytes(), &deck); err != nil {
		t.Fatalf("failed to parse the shared Deck: %v", err)
	}
	return deck
}

// A share link serves the Deck's Card total. Its reader is a guest, so no Review
// of theirs is Due and the Due count is zero.
func TestGetSharedDeckByTokenCarriesTheCardCount(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "하나", "one")
	due := dbtest.SeedCard(t, deckID, "둘", "two")
	dbtest.SeedDueReview(t, owner, due)

	token := readableDeckBody(t, h, owner, deckID).ShareToken

	got := sharedDeck(t, h, token)
	if got.CardCount != 2 {
		t.Fatalf("expected 2 live Cards, got %d", got.CardCount)
	}
	if got.DueCount != 0 {
		t.Fatalf("expected a guest to have no Due Cards, got %d", got.DueCount)
	}
}

// A single Deck read carries the same Card total and the caller's Due count as
// the listing does.
func TestGetDeckByIDCarriesCardAndDueCounts(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	due := dbtest.SeedCard(t, deckID, "지금", "due now")
	dbtest.SeedDueReview(t, owner, due)
	dbtest.SeedCard(t, deckID, "나중", "later")

	got := singleDeck(t, h, owner, deckID)
	if got.CardCount != 2 {
		t.Fatalf("expected 2 live Cards, got %d", got.CardCount)
	}
	if got.DueCount != 1 {
		t.Fatalf("expected 1 Due Card, got %d", got.DueCount)
	}

	guest := singleDeck(t, h, access.Anonymous, deckID)
	if guest.CardCount != 2 {
		t.Fatalf("expected a guest to read 2 live Cards, got %d", guest.CardCount)
	}
	if guest.DueCount != 0 {
		t.Fatalf("expected a guest to have no Due Cards, got %d", guest.DueCount)
	}
}

// The listing carries the Deck's Card total and the caller's Due count. Every
// live Card counts toward the total; only a stored Review due now counts as
// Due, and a deleted Card counts as neither.
func TestGetDecksListingCarriesCardAndDueCounts(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	due := dbtest.SeedCard(t, deckID, "지금", "due now")
	dbtest.SeedDueReview(t, owner, due)

	notDue := dbtest.SeedCard(t, deckID, "나중", "later")
	dbtest.SeedScheduledReview(t, owner, notDue, time.Now().Add(24*time.Hour))

	dbtest.SeedCard(t, deckID, "새것", "new")

	deleted := dbtest.SeedCard(t, deckID, "지운", "deleted")
	dbtest.SeedDueReview(t, owner, deleted)
	if err := dbtest.Queries(t).DeleteCard(context.Background(), deleted); err != nil {
		t.Fatalf("failed to delete the Card: %v", err)
	}

	got := listingDeck(t, h, owner, deckID)
	if got.CardCount != 3 {
		t.Fatalf("expected 3 live Cards, got %d", got.CardCount)
	}
	if got.DueCount != 1 {
		t.Fatalf("expected 1 Due Card, got %d", got.DueCount)
	}

	// A guest reads the same Deck and its Card total, but owns no Reviews, so
	// nothing in it is Due to them.
	guest := listingDeck(t, h, access.Anonymous, deckID)
	if guest.CardCount != 3 {
		t.Fatalf("expected a guest to read 3 live Cards, got %d", guest.CardCount)
	}
	if guest.DueCount != 0 {
		t.Fatalf("expected a guest to have no Due Cards, got %d", guest.DueCount)
	}
}

// A guest's listing answers from an entry. A Card write changes the Deck's Card
// total, so the entry holding the old total is cleared.
func TestGetDecksGuestListingRefreshesTheCardCountAfterACardWrite(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if got := listingDeck(t, h, access.Anonymous, deckID); got.CardCount != 1 {
		t.Fatalf("expected 1 live Card before the write, got %d", got.CardCount)
	}

	if w := del(t, h.DeleteCard, owner, cardRoute, cardPath(cardID)); w.Code != http.StatusAccepted {
		t.Fatalf("failed to delete the Card: %d: %s", w.Code, w.Body.String())
	}
	if got := listingDeck(t, h, access.Anonymous, deckID); got.CardCount != 0 {
		t.Fatalf("expected the deleted Card to leave the total, got %d", got.CardCount)
	}

	if w := post(t, h.CreateCards, owner, cardsRoute, cardsRoute, cardWriteBody(deckID, "숲", "forest")); w.Code != http.StatusCreated {
		t.Fatalf("failed to create the Card: %d: %s", w.Code, w.Body.String())
	}
	if got := listingDeck(t, h, access.Anonymous, deckID); got.CardCount != 1 {
		t.Fatalf("expected the created Card to join the total, got %d", got.CardCount)
	}
}

// A Deck with no Cards answers with zeros rather than dropping the fields.
func TestGetDecksListingCarriesZeroCountsForAnEmptyDeck(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	got := listingDeck(t, h, owner, deckID)
	if got.CardCount != 0 || got.DueCount != 0 {
		t.Fatalf("expected zeros for an empty Deck, got %+v", got)
	}
}
