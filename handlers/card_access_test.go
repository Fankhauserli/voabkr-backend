package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/types"
)

const cardsRoute = "/api/v1/cards/"

func cardsPath(deckID int64) string {
	return fmt.Sprintf("/api/v1/cards/?deck_id=%d", deckID)
}

// An anonymous caller asking for a private deck's Cards is refused, and a warm
// cards:deck:<id> entry does not answer for the deck.
func TestGetCardsOfPrivateDeckForbiddenForAnonymousCaller(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	seedCard(t, deckID, "나무", "tree")

	_ = h.Cache.SetProto(context.Background(), fmt.Sprintf("cards:deck:%d", deckID), &types.CardList{
		Cards: []*types.Card{{Id: 1, DeckId: uint32(deckID), KoreanWord: "나무", EnglishWord: "tree"}},
	}, 5*time.Minute)

	w := get(t, h.GetCards, access.Anonymous, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller reading a private deck's cards, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner of a private deck still reads its Cards.
func TestGetCardsOfPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	seedCard(t, deckID, "나무", "tree")

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

// A warm cards:deck:<id> entry still answers for a caller with access to the
// Deck — the cache is gated, not gone.
func TestGetCardsOfDeckServedFromWarmCache(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)

	_ = h.Cache.SetProto(context.Background(), fmt.Sprintf("cards:deck:%d", deckID), &types.CardList{
		Cards: []*types.Card{{Id: 1, DeckId: uint32(deckID), KoreanWord: "캐시", EnglishWord: "cache"}},
	}, 5*time.Minute)

	w := get(t, h.GetCards, owner, cardsRoute, cardsPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); !slices.Equal(got, []string{"캐시"}) {
		t.Fatalf("expected the cached card, got %v", got)
	}
}

// without a deck_id, the listing covers the decks the caller may read: public
// ones, their own, and the ones shared with them.
func TestGetCardsWithoutDeckFilterListsOnlyReadableDecks(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	stranger := seedUser(t, "stranger@example.com")

	publicDeck := seedDeck(t, owner, true)
	seedCard(t, publicDeck, "공개", "public")
	privateDeck := seedDeck(t, owner, false)
	seedCard(t, privateDeck, "비공개", "private")
	strangerDeck := seedDeck(t, stranger, false)
	seedCard(t, strangerDeck, "남의", "someone else's")
	seedShare(t, strangerDeck, owner, access.RoleView)

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

// An anonymous caller's listing is exactly the Cards of the public Decks the
// instance holds, whatever sits under cards:all: a warm entry — the shape the
// vulnerable handler wrote, another learner's private Cards included — neither
// reaches the listing nor empties it.
func TestGetCardsWithoutDeckFilterIgnoresWarmCacheEntry(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	publicDeck := seedDeck(t, owner, true)
	seedCard(t, publicDeck, "공개", "public")
	privateDeck := seedDeck(t, owner, false)
	seedCard(t, privateDeck, "비공개", "private")

	_ = h.Cache.SetProto(context.Background(), "cards:all", &types.CardList{
		Cards: []*types.Card{{Id: 1, DeckId: uint32(privateDeck), KoreanWord: "비공개", EnglishWord: "private"}},
	}, 5*time.Minute)

	w := get(t, h.GetCards, access.Anonymous, cardsRoute, "/api/v1/cards/")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for an anonymous caller, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); !slices.Equal(got, []string{"공개"}) {
		t.Fatalf("expected only the public Deck's Cards, got %v", got)
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

// A Card of a private deck is not readable by guessing its id, even when a
// card:<id> entry is warm.
func TestGetCardByIDInPrivateDeckForbiddenForAnonymousCaller(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")

	_ = h.Cache.SetProto(context.Background(), fmt.Sprintf("card:%d", cardID), &types.Card{
		Id:          uint32(cardID),
		DeckId:      uint32(deckID),
		KoreanWord:  "나무",
		EnglishWord: "tree",
	}, 5*time.Minute)

	w := get(t, h.GetCardByID, access.Anonymous, cardRoute, cardPath(cardID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller reading a private deck's card, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner of the private deck still reads its Cards one by one.
func TestGetCardByIDInPrivateDeckReadableByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")

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
