package handlers_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// cardHasSchedule reports whether a Card body carries the caller's Review
// schedule of it.
func cardHasSchedule(t *testing.T, body []byte) bool {
	t.Helper()

	var card types.Card
	if err := json.Unmarshal(body, &card); err != nil {
		t.Fatalf("failed to parse the Card: %v", err)
	}
	return card.ReviewSchedule != nil
}

// A Card added to a Deck leaves the entry the Deck's listing answers from: the
// listing a learner reads next carries it, rather than the Cards it held before.
func TestGetCardsAfterCreatingACard(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)

	if words := deckCardWords(t, h, owner, deckID); len(words) != 0 {
		t.Fatalf("expected an empty Deck to begin with, got %v", words)
	}

	w := post(t, h.CreateCards, owner, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create the Card: %d: %s", w.Code, w.Body.String())
	}

	if words := deckCardWords(t, h, owner, deckID); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the created Card in the listing, got %v", words)
	}
}

// A Card deleted leaves the entry the Deck's listing answers from.
func TestGetCardsAfterDeletingACard(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")

	if words := deckCardWords(t, h, owner, deckID); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the seeded Card to begin with, got %v", words)
	}

	w := del(t, h.DeleteCard, owner, cardRoute, cardPath(cardID))
	if w.Code != http.StatusAccepted {
		t.Fatalf("failed to delete the Card: %d: %s", w.Code, w.Body.String())
	}

	if words := deckCardWords(t, h, owner, deckID); len(words) != 0 {
		t.Fatalf("expected the deleted Card to leave the listing, got %v", words)
	}
}

// A Card the listings no longer carry is not readable by its id either: deleting
// a Card takes it out of every read path, not only the listings.
func TestGetCardByIDAfterTheCardIsDeleted(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")

	if w := get(t, h.GetCardByID, owner, cardRoute, cardPath(cardID)); w.Code != http.StatusOK {
		t.Fatalf("expected the seeded Card to be readable, got %d: %s", w.Code, w.Body.String())
	}

	w := del(t, h.DeleteCard, owner, cardRoute, cardPath(cardID))
	if w.Code != http.StatusAccepted {
		t.Fatalf("failed to delete the Card: %d: %s", w.Code, w.Body.String())
	}

	w = get(t, h.GetCardByID, owner, cardRoute, cardPath(cardID))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected the deleted Card to be gone, got %d: %s", w.Code, w.Body.String())
	}
}

// The single read carries the caller's own Review schedule of the Card, and a
// Card the caller has not studied carries none.
func TestGetCardByIDCarriesTheCallersSchedule(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, true)
	studied := seedCard(t, deckID, "배운", "studied")
	unstudied := seedCard(t, deckID, "안 배운", "unstudied")
	seedScheduledReview(t, owner, studied, time.Now().Add(24*time.Hour).Truncate(time.Second))

	tests := []struct {
		name      string
		cardID    int64
		wantSched bool
	}{
		{name: "a Card the caller has studied", cardID: studied, wantSched: true},
		{name: "a Card the caller has not studied", cardID: unstudied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := get(t, h.GetCardByID, owner, cardRoute, cardPath(tt.cardID))
			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			if hasSchedule := cardHasSchedule(t, w.Body.Bytes()); hasSchedule != tt.wantSched {
				t.Fatalf("expected a Review schedule: %v, got %v", tt.wantSched, hasSchedule)
			}
		})
	}
}

// The guest listing of a public Deck is the same Cards, with no schedule for a
// caller who has none.
func TestGetCardByIDForAGuest(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, true)
	cardID := seedCard(t, deckID, "공개", "public")

	w := get(t, h.GetCardByID, access.Anonymous, cardRoute, cardPath(cardID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for a guest, got %d: %s", w.Code, w.Body.String())
	}
	if cardHasSchedule(t, w.Body.Bytes()) {
		t.Fatal("expected a guest to carry no Review schedule")
	}
}
