package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
)

const sharedDeckRoute = "/api/v1/decks/shared/:token"

func sharedDeckPath(token string) string {
	return "/api/v1/decks/shared/" + token
}

// deckNameThroughLink reads the name of the Deck a share link serves, as a guest
// the link was handed to.
func deckNameThroughLink(t *testing.T, h *handlers.Handler, token string) string {
	t.Helper()

	w := get(t, h.GetSharedDeckByToken, access.Anonymous, sharedDeckRoute, sharedDeckPath(token))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the shared deck: %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse the shared deck: %v", err)
	}
	return body.Name
}

// A Deck renamed after its link was read serves the new name through the link:
// whatever answered the first read is an entry a write clears, not one the link
// keeps serving until it expires.
func TestGetSharedDeckByTokenServesARenamedDeck(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)
	token := readableDeckBody(t, h, owner, deckID).ShareToken

	if name := deckNameThroughLink(t, h, token); name != "Seeded Deck" {
		t.Fatalf("expected the seeded name through the link, got %q", name)
	}

	w := put(t, h.UpdateDeck, owner, deckRoute, deckPath(deckID), `{"name":"Renamed","type":"vocabulary"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to rename the deck: %d: %s", w.Code, w.Body.String())
	}

	if name := deckNameThroughLink(t, h, token); name != "Renamed" {
		t.Fatalf("expected the renamed deck through the link, got %q", name)
	}
}
