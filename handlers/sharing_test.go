package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// A share link is answered from the entry its first read wrote: a rename that
// went around the handler is not served until a write clears the entry.
func TestGetSharedDeckByTokenCacheHit(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := readableDeckBody(t, h, owner, deckID).ShareToken

	if got := sharedDeckThroughLink(t, h, token); got.Id != uint32(deckID) || got.Name != "Seeded Deck" || got.ShareToken != token {
		t.Fatalf("expected the seeded Deck through the link, got %+v", got)
	}

	renameDeckDirectly(t, h.DB, deckID, "Renamed")

	if got := sharedDeckThroughLink(t, h, token); got.Id != uint32(deckID) || got.Name != "Seeded Deck" || got.ShareToken != token {
		t.Fatalf("expected the entry to answer the second read, got %+v", got)
	}
}

// sharedDeckThroughLink reads the Deck a share link serves, as its holder does.
func sharedDeckThroughLink(t *testing.T, h *handlers.Handler, token string) *types.Deck {
	t.Helper()

	w := get(t, h.GetSharedDeckByToken, access.Anonymous, sharedDeckRoute, sharedDeckPath(token))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the shared deck: %d: %s", w.Code, w.Body.String())
	}

	var deck types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &deck); err != nil {
		t.Fatalf("failed to parse the shared deck: %v", err)
	}
	return &deck
}

func TestDeleteAccountUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.DELETE("/api/v1/user", h.DeleteAccount)

	req := httptest.NewRequest("DELETE", "/api/v1/user", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestShareDeckUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.POST("/api/v1/decks/:id/share", h.ShareDeck)

	req := httptest.NewRequest("POST", "/api/v1/decks/1/share", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}

func TestJoinSharedDeckUnauthorized(t *testing.T) {
	h := handlers.NewHandler(nil, nil)
	r := setupTestRouter()
	r.POST("/api/v1/decks/shared/:token/join", h.JoinSharedDeck)

	req := httptest.NewRequest("POST", "/api/v1/decks/shared/tok_123/join", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 Unauthorized, got %d: %s", w.Code, w.Body.String())
	}
}
