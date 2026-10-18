package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

const cardsRoute = "/api/v1/cards/"

func cardsPath(deckID int64) string {
	return fmt.Sprintf("/api/v1/cards/?deck_id=%d", deckID)
}

// An anonymous caller asking for a private deck's Cards is refused, and a warm
// entry does not answer for the deck: the Deck's standing is settled before the
// Cards are read.
func TestGetCardsOfPrivateDeckForbiddenForAnonymousCaller(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "나무", "tree")

	// The owner's read warms the Deck's entry.
	if w := get(t, h.GetCards, owner, cardsRoute, cardsPath(deckID)); w.Code != http.StatusOK {
		t.Fatalf("failed to warm the Deck's Cards: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetCards, access.Anonymous, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller reading a private deck's cards, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner of a private deck still reads its Cards.
func TestGetCardsOfPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "나무", "tree")

	w := get(t, h.GetCards, owner, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", w.Code, w.Body.String())
	}

	var cards []types.Card
	if err := json.Unmarshal(w.Body.Bytes(), &cards); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	words := make([]string, 0, len(cards))
	for i := range cards {
		words = append(words, cards[i].KoreanWord)
	}
	if len(words) != 1 || words[0] != "나무" {
		t.Fatalf("expected only the private deck's own card, got %v", words)
	}
}

// A warm entry still answers for a caller with access to the Deck — the cache
// is gated, not gone. The entry holds the words the first read wrote, not the
// change that went around the handler.
func TestGetCardsOfDeckServedFromWarmCache(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the seeded Card in the listing, got %v", got)
	}

	renameCardDirectly(t, h.DB, cardID, deckID, "숲", "forest")

	if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the warm entry's Card, got %v", got)
	}
}

// What a shared entry holds is the Deck's Cards; the caller's own Review schedule
// is attached to them on the way out, so a warm entry does not answer a learner
// as if they had never studied the Card.
func TestGetCardsFromWarmCacheCarriesTheCallersSchedule(t *testing.T) {
	h := newTestHandlerWithCache(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "배운", "studied")

	nextReviewAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	dbtest.SeedScheduledReview(t, learner, cardID, nextReviewAt)

	// The first read warms the entry with the Card as it is now.
	if got := deckCardWords(t, h, learner, deckID); !slices.Equal(got, []string{"배운"}) {
		t.Fatalf("expected the studied Card in the listing, got %v", got)
	}

	renameCardDirectly(t, h.DB, cardID, deckID, "바뀐", "changed")

	w := get(t, h.GetCards, learner, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var cards []cardSchedule
	if err := json.Unmarshal(w.Body.Bytes(), &cards); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if len(cards) != 1 || cards[0].KoreanWord != "배운" {
		t.Fatalf("expected the Card the entry holds, got %s", w.Body.String())
	}
	if cards[0].ReviewSchedule == nil {
		t.Fatalf("expected the learner's Review schedule attached to the cached Card, got %s", w.Body.String())
	}
	if cards[0].ReviewSchedule.NextReviewAt != rfc3339(nextReviewAt) {
		t.Fatalf("expected nextReviewAt %s, got %q", rfc3339(nextReviewAt), cards[0].ReviewSchedule.NextReviewAt)
	}
}

// without a deck_id, the listing covers the decks the caller may read: public
// ones, their own, and the ones shared with them.
func TestGetCardsWithoutDeckFilterListsOnlyReadableDecks(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	stranger := dbtest.SeedUser(t, "stranger@example.com")

	publicDeck := dbtest.SeedDeck(t, owner, true)
	dbtest.SeedCard(t, publicDeck, "공개", "public")
	privateDeck := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, privateDeck, "비공개", "private")
	strangerDeck := dbtest.SeedDeck(t, stranger, false)
	dbtest.SeedCard(t, strangerDeck, "남의", "someone else's")
	dbtest.SeedShare(t, strangerDeck, owner, access.RoleView)

	tests := []struct {
		name   string
		caller int64
		want   []string
	}{
		{name: "anonymous", caller: access.Anonymous, want: []string{"공개"}},
		{name: "owner", caller: owner, want: []string{"공개", "비공개", "남의"}},
		{name: "stranger", caller: stranger, want: []string{"공개", "남의"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := get(t, h.GetCards, tt.caller, cardsRoute, "/api/v1/cards/")
			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}

			got := cardWords(t, w.Body.Bytes())
			if !slices.Equal(got, tt.want) {
				t.Fatalf("expected cards %v, got %v", tt.want, got)
			}
		})
	}
}

func cardWords(t *testing.T, body []byte) []string {
	t.Helper()

	var cards []types.Card
	if err := json.Unmarshal(body, &cards); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	words := make([]string, 0, len(cards))
	for i := range cards {
		words = append(words, cards[i].KoreanWord)
	}
	return words
}

const cardRoute = "/api/v1/cards/:id"

func cardPath(cardID int64) string {
	return fmt.Sprintf("/api/v1/cards/%d", cardID)
}

// A Card of a private deck is not readable by guessing its id, even after a
// caller who may read the Deck asked for it.
func TestGetCardByIDInPrivateDeckForbiddenForAnonymousCaller(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if w := get(t, h.GetCardByID, owner, cardRoute, cardPath(cardID)); w.Code != http.StatusOK {
		t.Fatalf("failed to read the private deck's Card as its owner: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetCardByID, access.Anonymous, cardRoute, cardPath(cardID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller reading a private deck's card, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner of the private deck still reads its Cards one by one.
func TestGetCardByIDInPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	w := get(t, h.GetCardByID, owner, cardRoute, cardPath(cardID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", w.Code, w.Body.String())
	}

	var card types.Card
	if err := json.Unmarshal(w.Body.Bytes(), &card); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if card.KoreanWord != "나무" {
		t.Fatalf("expected the private deck's own card, got %q", card.KoreanWord)
	}
}
