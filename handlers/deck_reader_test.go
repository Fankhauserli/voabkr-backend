package handlers_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
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
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
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

// deckNames returns the names in the listing caller reads, warming whatever
// answers it.
func deckNames(t *testing.T, h *handlers.Handler, caller int64) []string {
	t.Helper()

	w := get(t, h.GetDecks, caller, decksRoute, decksRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the listing as %d: %d: %s", caller, w.Code, w.Body.String())
	}

	var decks []types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &decks); err != nil {
		t.Fatalf("failed to parse the listing: %v", err)
	}

	names := make([]string, 0, len(decks))
	for i := range decks {
		names = append(names, decks[i].Name)
	}
	return names
}

// assertEmptyDeckListing checks that the listing caller reads is the empty
// array the API contract promises, whichever path answered it.
func assertEmptyDeckListing(t *testing.T, h *handlers.Handler, caller int64, read string) {
	t.Helper()

	w := get(t, h.GetDecks, caller, decksRoute, decksRoute)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the %s listing: %d: %s", read, w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "[]" {
		t.Fatalf("expected the %s listing to be [], got %s", read, got)
	}
}

// An empty listing is the empty array the API contract promises, whichever path
// answers it: the database read, the entry its first request wrote, and the
// listing a signed-in learner with no Decks reads.
func TestGetDecksEmptyListingIsAnEmptyArray(t *testing.T) {
	h := newTestHandlerWithCache(t)

	learner := dbtest.SeedUser(t, "learner@example.com")
	assertEmptyDeckListing(t, h, learner, "signed-in")

	// The guest's first request reads the database and writes its empty answer
	// to the entry.
	assertEmptyDeckListing(t, h, access.Anonymous, "cache-miss")

	// A public Deck seeded through the database alone hides behind the entry
	// the first request wrote, so the second request is answered from it.
	owner := dbtest.SeedUser(t, "owner@example.com")
	dbtest.SeedDeck(t, owner, true)
	assertEmptyDeckListing(t, h, access.Anonymous, "cache-hit")
}

// A Deck deleted after its link was read is gone through the link too.
func TestGetSharedDeckByTokenAfterTheDeckIsDeleted(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := readableDeckBody(t, h, owner, deckID).ShareToken

	if name := deckNameThroughLink(t, h, token); name != "Seeded Deck" {
		t.Fatalf("expected the seeded name through the link, got %q", name)
	}

	w := del(t, h.DeleteDeck, owner, deckRoute, deckPath(deckID))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to delete the deck: %d: %s", w.Code, w.Body.String())
	}

	w = get(t, h.GetSharedDeckByToken, access.Anonymous, sharedDeckRoute, sharedDeckPath(token))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected the deleted deck to be gone through its link, got %d: %s", w.Code, w.Body.String())
	}
}

// The listing carries the Access role the caller reads each Deck under, resolved
// by the same readable-Decks rule the Card and Review reads select from.
func TestGetDecksListingCarriesTheCallersRole(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	viewer := dbtest.SeedUser(t, "viewer@example.com")
	editor := dbtest.SeedUser(t, "editor@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	publicDeck := dbtest.SeedDeck(t, owner, true)
	dbtest.SeedShare(t, deckID, viewer, access.RoleView)
	dbtest.SeedShare(t, deckID, editor, access.RoleEdit)

	tests := []struct {
		name   string
		caller int64
		deck   int64
		want   string
	}{
		{name: "the owner reads owner", caller: owner, deck: deckID, want: access.RoleOwner},
		{name: "a view holder reads view", caller: viewer, deck: deckID, want: access.RoleView},
		{name: "an edit holder reads edit", caller: editor, deck: deckID, want: access.RoleEdit},
		{name: "a guest reads public", caller: access.Anonymous, deck: publicDeck, want: access.RolePublic},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := get(t, h.GetDecks, tt.caller, decksRoute, decksRoute)
			if w.Code != http.StatusOK {
				t.Fatalf("failed to read the listing: %d: %s", w.Code, w.Body.String())
			}

			var decks []types.Deck
			if err := json.Unmarshal(w.Body.Bytes(), &decks); err != nil {
				t.Fatalf("failed to parse the listing: %v", err)
			}

			for i := range decks {
				if int64(decks[i].Id) == tt.deck {
					if decks[i].UserAccessRole != tt.want {
						t.Fatalf("expected role %q, got %q", tt.want, decks[i].UserAccessRole)
					}
					return
				}
			}
			t.Fatalf("expected Deck %d in the listing, got %v", tt.deck, decks)
		})
	}
}

// A revoked DeckShare leaves the holder's listing on their next request, not at
// the entry's expiry.
func TestGetDecksAfterADeckShareIsRevoked(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedShare(t, deckID, learner, access.RoleView)

	if names := deckNames(t, h, learner); !slices.Contains(names, "Seeded Deck") {
		t.Fatalf("expected the shared deck in the listing, got %v", names)
	}

	w := del(t, h.RevokeDeckShare, owner, deckRevokeRoute, deckRevokePath(deckID, learner))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to revoke the share: %d: %s", w.Code, w.Body.String())
	}

	if names := deckNames(t, h, learner); slices.Contains(names, "Seeded Deck") {
		t.Fatalf("expected the revoked deck to leave the listing, got %v", names)
	}
}

// A Deck joined through its link appears in the joiner's listing on their next
// request.
func TestGetDecksAfterJoiningASharedDeck(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := readableDeckBody(t, h, owner, deckID).ShareToken

	if names := deckNames(t, h, learner); len(names) != 0 {
		t.Fatalf("expected an empty listing to begin with, got %v", names)
	}

	w := post(t, h.JoinSharedDeck, learner, joinRoute, joinPath(token), "")
	if w.Code != http.StatusOK {
		t.Fatalf("failed to join the shared deck: %d: %s", w.Code, w.Body.String())
	}

	if names := deckNames(t, h, learner); !slices.Contains(names, "Seeded Deck") {
		t.Fatalf("expected the joined deck in the listing, got %v", names)
	}
}

// A Deck outlives the learner it belonged to — erasing them leaves it owned by no
// one — and the link answers from the row rather than an entry that outlived the
// erasure, so no link still names them.
func TestGetSharedDeckByTokenAfterTheOwnerIsErased(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	token := readableDeckBody(t, h, owner, deckID).ShareToken

	if name := deckNameThroughLink(t, h, token); name != "Seeded Deck" {
		t.Fatalf("expected the seeded name through the link, got %q", name)
	}

	w := del(t, h.DeleteAccount, owner, "/api/v1/user", "/api/v1/user")
	if w.Code != http.StatusOK {
		t.Fatalf("failed to erase the account: %d: %s", w.Code, w.Body.String())
	}

	w = get(t, h.GetSharedDeckByToken, access.Anonymous, sharedDeckRoute, sharedDeckPath(token))
	if w.Code != http.StatusOK {
		t.Fatalf("expected the Deck to outlive its owner, got %d: %s", w.Code, w.Body.String())
	}

	var body struct {
		OwnerName string `json:"ownerName"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to parse the shared deck: %v", err)
	}
	if body.OwnerName != "" {
		t.Fatalf("expected no owner named through the link, got %q", body.OwnerName)
	}
}

// Sharing a Deck puts it in the invited learner's listing on their next request.
func TestGetDecksAfterAShare(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)

	if names := deckNames(t, h, learner); len(names) != 0 {
		t.Fatalf("expected an empty listing to begin with, got %v", names)
	}

	w := post(t, h.ShareDeck, owner, deckShareRoute, deckSharePath(deckID), shareBody("learner@example.com", access.RoleView))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to share the deck: %d: %s", w.Code, w.Body.String())
	}

	if names := deckNames(t, h, learner); !slices.Contains(names, "Seeded Deck") {
		t.Fatalf("expected the shared deck in the invited learner's listing, got %v", names)
	}
}

// A guest's listing is answered from an entry, and a write clears it: the renamed
// Deck is served under its new name rather than the one the entry holds.
func TestGetDecksForGuestsAfterARename(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	deckID := dbtest.SeedDeck(t, owner, true)

	if names := deckNames(t, h, access.Anonymous); !slices.Contains(names, "Seeded Deck") {
		t.Fatalf("expected the public deck in the guest listing, got %v", names)
	}

	w := put(t, h.UpdateDeck, owner, deckRoute, deckPath(deckID), `{"name":"Renamed","type":"vocabulary","isPublic":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to rename the deck: %d: %s", w.Code, w.Body.String())
	}

	if names := deckNames(t, h, access.Anonymous); !slices.Contains(names, "Renamed") {
		t.Fatalf("expected the renamed deck in the guest listing, got %v", names)
	}
}

// A Deck created appears in its owner's listing on their next request.
func TestGetDecksAfterCreatingADeck(t *testing.T) {
	h := newTestHandlerWithCache(t)
	owner := dbtest.SeedUser(t, "owner@example.com")

	if names := deckNames(t, h, owner); len(names) != 0 {
		t.Fatalf("expected an empty listing to begin with, got %v", names)
	}

	w := post(t, h.CreateDeck, owner, decksRoute, decksRoute, `{"name":"New Deck","type":"vocabulary"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create the deck: %d: %s", w.Code, w.Body.String())
	}

	if names := deckNames(t, h, owner); !slices.Contains(names, "New Deck") {
		t.Fatalf("expected the created deck in the listing, got %v", names)
	}
}

const joinRoute = "/api/v1/decks/shared/:token/join"

func joinPath(token string) string {
	return "/api/v1/decks/shared/" + token + "/join"
}
