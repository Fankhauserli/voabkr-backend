package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

const deckRoute = "/api/v1/decks/:id"

func deckPath(deckID int64) string {
	return fmt.Sprintf("/api/v1/decks/%d", deckID)
}

// A poisoned deck:<id> entry — the shape the vulnerable handler wrote, another
// caller's role and the share token included — must not be served to an anonymous caller.
func TestGetDeckByIDKeepsPrivateDeckPrivateThroughCache(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)

	_ = h.Cache.SetProto(context.Background(), fmt.Sprintf("deck:%d", deckID), &types.Deck{
		Id:             uint32(deckID),
		Name:           "Seeded Deck",
		Type:           "vocabulary",
		OwnerId:        uint32(owner),
		IsPublic:       false,
		ShareToken:     "sec_seed_1",
		UserAccessRole: "owner",
	}, 5*time.Minute)

	w := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(deckID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller reading a private deck, got %d: %s", w.Code, w.Body.String())
	}
}

// The owner's own fetch must not leave anything behind that an anonymous caller can read.
func TestGetDeckByIDPrivateDeckStaysForbiddenAfterOwnerFetch(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)

	ownerRecorder := get(t, h.GetDeckByID, owner, deckRoute, deckPath(deckID))
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", ownerRecorder.Code, ownerRecorder.Body.String())
	}

	anonymousRecorder := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(deckID))

	if anonymousRecorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for an anonymous caller after the owner's fetch, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}
}

// An anonymous caller reading a public deck is served their own Access role,
// never the role of whichever caller's fetch went first.
func TestGetDeckByIDServesTheCallersOwnAccessRole(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	ownerRecorder := get(t, h.GetDeckByID, owner, deckRoute, deckPath(deckID))
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 for the owner, got %d: %s", ownerRecorder.Code, ownerRecorder.Body.String())
	}

	anonymousRecorder := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(deckID))
	if anonymousRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 for an anonymous caller reading a public deck, got %d: %s", anonymousRecorder.Code, anonymousRecorder.Body.String())
	}

	var anonymousDeck types.Deck
	if err := json.Unmarshal(anonymousRecorder.Body.Bytes(), &anonymousDeck); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if anonymousDeck.UserAccessRole != access.RolePublic {
		t.Fatalf("expected the anonymous caller's own access role %q, got %q", access.RolePublic, anonymousDeck.UserAccessRole)
	}
}

// A learner holding a view share on a private deck may read it.
func TestGetDeckByIDPrivateDeckReadableByShareHolder(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedShare(t, deckID, learner, access.RoleView)

	w := get(t, h.GetDeckByID, learner, deckRoute, deckPath(deckID))

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for a view holder, got %d: %s", w.Code, w.Body.String())
	}

	var deck types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &deck); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if deck.UserAccessRole != access.RoleView {
		t.Fatalf("expected access role %q, got %q", access.RoleView, deck.UserAccessRole)
	}
}

func TestGetDeckByIDMissingDeckNotFound(t *testing.T) {
	h := newTestHandler(t)

	w := get(t, h.GetDeckByID, access.Anonymous, deckRoute, deckPath(999))

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for a missing deck, got %d: %s", w.Code, w.Body.String())
	}
}

const decksRoute = "/api/v1/decks/"

// An anonymous caller's listing is exactly the public Decks the instance holds,
// whatever sits under decks:all: a private Deck's Access role and share token in a
// warm entry neither reaches the listing nor empties it. Which Decks a caller may
// list is a decision about the caller, so only decks:public, an entry holding
// public Decks alone, serves it.
func TestGetDecksForAnonymousCallerIgnoresWarmDecksAllEntry(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	publicDeckID := dbtest.SeedDeck(t, owner, true)
	privateDeckID := dbtest.SeedDeck(t, owner, false)

	_ = h.Cache.SetProto(context.Background(), "decks:all", &types.DeckList{Decks: []*types.Deck{{
		Id:             uint32(privateDeckID),
		Name:           "Warm Private Deck",
		Type:           "vocabulary",
		OwnerId:        uint32(owner),
		IsPublic:       false,
		ShareToken:     "sec_seed_1",
		UserAccessRole: access.RoleOwner,
	}}}, 5*time.Minute)

	w := get(t, h.GetDecks, access.Anonymous, decksRoute, decksRoute)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for an anonymous caller, got %d: %s", w.Code, w.Body.String())
	}

	var decks []types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &decks); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	if len(decks) != 1 || decks[0].Id != uint32(publicDeckID) {
		t.Fatalf("expected the public Deck and nothing else, got %s", w.Body.String())
	}
}
