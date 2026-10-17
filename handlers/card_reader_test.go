package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
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
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)

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

// Editing a Card in place leaves the entry the Deck's listing answers from.
func TestGetCardsAfterUpdatingACard(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

	if words := deckCardWords(t, h, owner, deckID); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the seeded Card to begin with, got %v", words)
	}

	w := put(t, h.UpdateCard, owner, cardRoute, cardPath(cardID), cardWriteBody(deckID, "숲", "forest"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("failed to update the Card: %d: %s", w.Code, w.Body.String())
	}

	if words := deckCardWords(t, h, owner, deckID); !slices.Equal(words, []string{"숲"}) {
		t.Fatalf("expected the edited Card in the listing, got %v", words)
	}
}

// A Card deleted leaves the entry the Deck's listing answers from.
func TestGetCardsAfterDeletingACard(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

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
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, deckID, "나무", "tree")

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

// Deleting a Card takes it out of the Review queue too: the Card is gone from
// every read path, not only the by-id one.
func TestGetReviewsAfterTheCardIsDeleted(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	dbtest.SeedDueReview(t, learner, cardID)

	if got := cardWords(t, get(t, h.GetReviews, learner, reviewsRoute, reviewsRoute).Body.Bytes()); !slices.Equal(got, []string{"지운"}) {
		t.Fatalf("expected the due Card in the queue to begin with, got %v", got)
	}

	if w := del(t, h.DeleteCard, learner, cardRoute, cardPath(cardID)); w.Code != http.StatusAccepted {
		t.Fatalf("failed to delete the Card: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetReviews, learner, reviewsRoute, reviewsRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the deleted Card to leave the queue, got %v", got)
	}
}

// Deleting a Deck takes its Cards out of the cross-Deck listing: the listing
// applies the same liveness rule as a single-Deck read.
func TestGetCardsListingDropsCardsOfDeletedDeck(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedCard(t, deckID, "지운", "deleted")

	if got := listedWords(t, h, owner, cardsRoute); !slices.Equal(got, []string{"지운"}) {
		t.Fatalf("expected the Deck's Card in the listing to begin with, got %v", got)
	}

	if w := del(t, h.DeleteDeck, owner, deckRoute, deckPath(deckID)); w.Code != http.StatusOK {
		t.Fatalf("failed to delete the Deck: %d: %s", w.Code, w.Body.String())
	}

	if got := listedWords(t, h, owner, cardsRoute); len(got) != 0 {
		t.Fatalf("expected the deleted Deck's Card to leave the listing, got %v", got)
	}
}

// The Review queue applies the same liveness rule: deleting a Deck takes its
// Cards out of the queue even though the Reviews themselves remain.
func TestGetReviewsQueueDropsCardsOfDeletedDeck(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	dbtest.SeedDueReview(t, learner, cardID)

	if got := cardWords(t, get(t, h.GetReviews, learner, reviewsRoute, reviewsRoute).Body.Bytes()); !slices.Equal(got, []string{"지운"}) {
		t.Fatalf("expected the due Card in the queue to begin with, got %v", got)
	}

	if w := del(t, h.DeleteDeck, learner, deckRoute, deckPath(deckID)); w.Code != http.StatusOK {
		t.Fatalf("failed to delete the Deck: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetReviews, learner, reviewsRoute, reviewsRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the deleted Deck's Card to leave the queue, got %v", got)
	}
}

// The since listing selects from the same rule, so a deleted Deck leaves it too.
func TestGetReviewsSinceDropsCardsOfDeletedDeck(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	dbtest.SeedDueReview(t, learner, cardID)

	since := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	path := fmt.Sprintf("/api/v1/reviews/since/%s", since)

	if got := cardWords(t, get(t, h.GetReviewsSince, learner, "/api/v1/reviews/since/:time", path).Body.Bytes()); !slices.Equal(got, []string{"지운"}) {
		t.Fatalf("expected the due Card in the listing to begin with, got %v", got)
	}

	if w := del(t, h.DeleteDeck, learner, deckRoute, deckPath(deckID)); w.Code != http.StatusOK {
		t.Fatalf("failed to delete the Deck: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetReviewsSince, learner, "/api/v1/reviews/since/:time", path)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the deleted Deck's Card to leave the listing, got %v", got)
	}
}

// The since listing reads a deleted Card as gone for the same reason.
func TestGetReviewsSinceAfterTheCardIsDeleted(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, learner, false)
	cardID := dbtest.SeedCard(t, deckID, "지운", "deleted")
	dbtest.SeedDueReview(t, learner, cardID)

	since := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	path := fmt.Sprintf("/api/v1/reviews/since/%s", since)

	if got := cardWords(t, get(t, h.GetReviewsSince, learner, "/api/v1/reviews/since/:time", path).Body.Bytes()); !slices.Equal(got, []string{"지운"}) {
		t.Fatalf("expected the due Card in the listing to begin with, got %v", got)
	}

	if w := del(t, h.DeleteCard, learner, cardRoute, cardPath(cardID)); w.Code != http.StatusAccepted {
		t.Fatalf("failed to delete the Card: %d: %s", w.Code, w.Body.String())
	}

	w := get(t, h.GetReviewsSince, learner, "/api/v1/reviews/since/:time", path)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := cardWords(t, w.Body.Bytes()); len(got) != 0 {
		t.Fatalf("expected the deleted Card to leave the listing, got %v", got)
	}
}

// The single read carries the caller's own Review schedule of the Card, and a
// Card the caller has not studied carries none.
func TestGetCardByIDCarriesTheCallersSchedule(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	studied := dbtest.SeedCard(t, deckID, "배운", "studied")
	unstudied := dbtest.SeedCard(t, deckID, "안 배운", "unstudied")
	dbtest.SeedScheduledReview(t, owner, studied, time.Now().Add(24*time.Hour).Truncate(time.Second))

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

// A paged listing is answered from an entry of its own, and a write clears that
// too: the window a learner reads next is the Deck's Cards as they are now.
func TestGetCardsPagedAfterCreatingACard(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	paged := cardsPath(deckID) + "&limit=1&offset=0"

	if words := listedWords(t, h, owner, paged); len(words) != 0 {
		t.Fatalf("expected an empty window to begin with, got %v", words)
	}

	w := post(t, h.CreateCards, owner, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create the Card: %d: %s", w.Code, w.Body.String())
	}

	if words := listedWords(t, h, owner, paged); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the created Card in the window, got %v", words)
	}
}

// Moving a Card between Decks leaves neither Deck's entry stale.
func TestGetCardsAfterMovingACardBetweenDecks(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	fromDeck := dbtest.SeedDeck(t, owner, false)
	toDeck := dbtest.SeedDeck(t, owner, false)
	cardID := dbtest.SeedCard(t, fromDeck, "나무", "tree")
	dbtest.SeedShare(t, fromDeck, learner, access.RoleEdit)
	dbtest.SeedShare(t, toDeck, learner, access.RoleEdit)

	if words := deckCardWords(t, h, learner, fromDeck); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the Card in the Deck that holds it, got %v", words)
	}
	if words := deckCardWords(t, h, learner, toDeck); len(words) != 0 {
		t.Fatalf("expected the other Deck to be empty, got %v", words)
	}

	w := put(t, h.UpdateCard, learner, cardRoute, cardPath(cardID), cardWriteBody(toDeck, "나무", "tree"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("failed to move the Card: %d: %s", w.Code, w.Body.String())
	}

	if words := deckCardWords(t, h, learner, fromDeck); len(words) != 0 {
		t.Fatalf("expected the moved Card to leave its old Deck, got %v", words)
	}
	if words := deckCardWords(t, h, learner, toDeck); !slices.Equal(words, []string{"나무"}) {
		t.Fatalf("expected the moved Card in its new Deck, got %v", words)
	}
}

// A Card that is not there is not found — a failure to read it would be a
// different answer.
func TestGetCardByIDMissingCardNotFound(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	dbtest.SeedDeck(t, owner, false)

	w := get(t, h.GetCardByID, owner, cardRoute, cardPath(99999))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for a Card that is not there, got %d: %s", w.Code, w.Body.String())
	}
}

// listedWords returns the words a listing path answers with.
func listedWords(t *testing.T, h *handlers.Handler, caller int64, path string) []string {
	t.Helper()

	w := get(t, h.GetCards, caller, cardsRoute, path)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the listing: %d: %s", w.Code, w.Body.String())
	}
	return cardWords(t, w.Body.Bytes())
}

// The guest listing of a public Deck is the same Cards, with no schedule for a
// caller who has none.
func TestGetCardByIDForAGuest(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)
	cardID := dbtest.SeedCard(t, deckID, "공개", "public")

	w := get(t, h.GetCardByID, access.Anonymous, cardRoute, cardPath(cardID))
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for a guest, got %d: %s", w.Code, w.Body.String())
	}
	if cardHasSchedule(t, w.Body.Bytes()) {
		t.Fatal("expected a guest to carry no Review schedule")
	}
}
