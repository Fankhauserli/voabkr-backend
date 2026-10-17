package handlers_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/internal/dbtest"
	"github.com/Fankhauserli/voabkr-backend/types"
)

// deckReadable reports whether reader can still read deckID. A deleted Deck
// answers as though it never existed, so this is what a refused delete must
// leave behind.
func deckReadable(t *testing.T, h *handlers.Handler, reader, deckID int64) bool {
	t.Helper()

	switch w := get(t, h.GetDeckByID, reader, deckRoute, deckPath(deckID)); w.Code {
	case http.StatusOK:
		return true
	case http.StatusForbidden, http.StatusNotFound:
		return false
	default:
		t.Fatalf("unexpected status %d reading deck %d: %s", w.Code, deckID, w.Body.String())
		return false
	}
}

// Deleting a Deck is the owner's act alone: a holder of an edit Permission and a
// learner with no standing are both refused, and the Deck stays.
func TestDeleteDeckRefusedToEveryoneButTheOwner(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		asOwner    bool
		want       int
		wantGone   bool
	}{
		{name: "owner", asOwner: true, want: http.StatusOK, wantGone: true},
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusForbidden},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden},
		{name: "no standing", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := dbtest.SeedUser(t, "owner@example.com")
			caller := dbtest.SeedUser(t, "learner@example.com")
			if tt.asOwner {
				caller = owner
			}
			deckID := dbtest.SeedDeck(t, owner, false)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, caller, tt.permission)
			}

			w := del(t, h.DeleteDeck, caller, deckRoute, deckPath(deckID))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if gone := !deckReadable(t, h, owner, deckID); gone != tt.wantGone {
				t.Fatalf("expected the deck to be gone: %v, got %v", tt.wantGone, gone)
			}
		})
	}
}

// A System deck is deleted by no one: it has no owner for anyone to be, so even
// the strongest standing on it — a share carrying edit — is refused and the Deck
// stays.
func TestDeleteSystemDeckRefused(t *testing.T) {
	tests := []struct {
		name       string
		permission string
	}{
		{name: "no standing"},
		{name: "edit holder", permission: access.RoleEdit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			deckID := dbtest.SeedDeck(t, 0, true)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, learner, tt.permission)
			}

			w := del(t, h.DeleteDeck, learner, deckRoute, deckPath(deckID))

			if w.Code != http.StatusForbidden {
				t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
			}
			if !deckReadable(t, h, access.Anonymous, deckID) {
				t.Fatal("expected the refused delete to leave the System deck in place")
			}
		})
	}
}

const deckShareRoute = "/api/v1/decks/:id/share"

func deckSharePath(deckID int64) string {
	return fmt.Sprintf("/api/v1/decks/%d/share", deckID)
}

// shareBody invites a learner by email at a Permission.
func shareBody(email, permission string) string {
	return fmt.Sprintf(`{"email":%q,"permission":%q}`, email, permission)
}

// Sharing a Deck is the owner's act alone: an invitation from anyone else grants
// the invited learner nothing.
func TestShareDeckRefusedToEveryoneButTheOwner(t *testing.T) {
	tests := []struct {
		name             string
		permission       string
		asOwner          bool
		want             int
		wantInvitedReads bool
	}{
		{name: "owner", asOwner: true, want: http.StatusOK, wantInvitedReads: true},
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusForbidden},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden},
		{name: "no standing", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := dbtest.SeedUser(t, "owner@example.com")
			caller := dbtest.SeedUser(t, "learner@example.com")
			invited := dbtest.SeedUser(t, "invited@example.com")
			if tt.asOwner {
				caller = owner
			}
			deckID := dbtest.SeedDeck(t, owner, false)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, caller, tt.permission)
			}

			w := post(t, h.ShareDeck, caller, deckShareRoute, deckSharePath(deckID), shareBody("invited@example.com", access.RoleView))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if reads := deckReadable(t, h, invited, deckID); reads != tt.wantInvitedReads {
				t.Fatalf("expected the invited learner to read the deck: %v, got %v", tt.wantInvitedReads, reads)
			}
		})
	}
}

// A System deck is shared by no one, whatever standing the caller holds on it,
// and a refused share grants the invited learner nothing.
func TestShareSystemDeckRefused(t *testing.T) {
	tests := []struct {
		name       string
		permission string
	}{
		{name: "no standing"},
		{name: "edit holder", permission: access.RoleEdit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			invited := dbtest.SeedUser(t, "invited@example.com")
			deckID := dbtest.SeedDeck(t, 0, false)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, learner, tt.permission)
			}

			w := post(t, h.ShareDeck, learner, deckShareRoute, deckSharePath(deckID), shareBody("invited@example.com", access.RoleView))

			if w.Code != http.StatusForbidden {
				t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
			}
			if deckReadable(t, h, invited, deckID) {
				t.Fatal("expected the refused share to grant the invited learner nothing")
			}
		})
	}
}

const deckRevokeRoute = "/api/v1/decks/:id/share/:user_id"

func deckRevokePath(deckID, userID int64) string {
	return fmt.Sprintf("/api/v1/decks/%d/share/%d", deckID, userID)
}

// Revoking a DeckShare is the owner's act alone: a refusal leaves the holder
// reading the Deck as they did.
func TestRevokeDeckShareRefusedToEveryoneButTheOwner(t *testing.T) {
	tests := []struct {
		name        string
		permission  string
		asOwner     bool
		want        int
		wantRevoked bool
	}{
		{name: "owner", asOwner: true, want: http.StatusOK, wantRevoked: true},
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusForbidden},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden},
		{name: "no standing", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := dbtest.SeedUser(t, "owner@example.com")
			caller := dbtest.SeedUser(t, "learner@example.com")
			holder := dbtest.SeedUser(t, "holder@example.com")
			if tt.asOwner {
				caller = owner
			}
			deckID := dbtest.SeedDeck(t, owner, false)
			dbtest.SeedShare(t, deckID, holder, access.RoleView)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, caller, tt.permission)
			}

			w := del(t, h.RevokeDeckShare, caller, deckRevokeRoute, deckRevokePath(deckID, holder))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if revoked := !deckReadable(t, h, holder, deckID); revoked != tt.wantRevoked {
				t.Fatalf("expected the holder's access revoked: %v, got %v", tt.wantRevoked, revoked)
			}
		})
	}
}

// A System deck's DeckShares are revoked by no one, whatever standing the caller
// holds on it: the holder reads it as before.
func TestRevokeSystemDeckShareRefused(t *testing.T) {
	tests := []struct {
		name       string
		permission string
	}{
		{name: "no standing"},
		{name: "edit holder", permission: access.RoleEdit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			learner := dbtest.SeedUser(t, "learner@example.com")
			holder := dbtest.SeedUser(t, "holder@example.com")
			deckID := dbtest.SeedDeck(t, 0, false)
			dbtest.SeedShare(t, deckID, holder, access.RoleView)
			if tt.permission != "" {
				dbtest.SeedShare(t, deckID, learner, tt.permission)
			}

			w := del(t, h.RevokeDeckShare, learner, deckRevokeRoute, deckRevokePath(deckID, holder))

			if w.Code != http.StatusForbidden {
				t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
			}
			if !deckReadable(t, h, holder, deckID) {
				t.Fatal("expected the refused revoke to leave the DeckShare in place")
			}
		})
	}
}

// A System deck's name and type are changed by no one: the Deck is maintained,
// never edited through the application, so a share on it carries no edit.
func TestUpdateSystemDeckRefused(t *testing.T) {
	h := newTestHandler(t)
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, 0, false)
	dbtest.SeedShare(t, deckID, learner, access.RoleEdit)

	w := put(t, h.UpdateDeck, learner, deckRoute, deckPath(deckID), `{"name":"Renamed","type":"grammar"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
	}
	if deck := readableDeckBody(t, h, learner, deckID); deck.Name != "Seeded Deck" {
		t.Fatalf("expected the refused update to leave the name alone, got %q", deck.Name)
	}
}

// What an edit Permission still carries: the Deck's name and type may be
// changed, but not its visibility, which stays the owner's to set.
func TestUpdateDeckNameAndTypeByEditHolder(t *testing.T) {
	h := newTestHandler(t)
	owner := dbtest.SeedUser(t, "owner@example.com")
	learner := dbtest.SeedUser(t, "learner@example.com")
	deckID := dbtest.SeedDeck(t, owner, false)
	dbtest.SeedShare(t, deckID, learner, access.RoleEdit)

	w := put(t, h.UpdateDeck, learner, deckRoute, deckPath(deckID), `{"name":"Renamed","type":"grammar","isPublic":true}`)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 for an edit holder, got %d: %s", w.Code, w.Body.String())
	}

	deck := readableDeckBody(t, h, owner, deckID)
	if deck.Name != "Renamed" || deck.Type != "grammar" {
		t.Fatalf("expected the edit holder's name and type to stick, got %q / %q", deck.Name, deck.Type)
	}
	if deck.IsPublic {
		t.Fatal("expected visibility to stay the owner's to set")
	}
}

// readableDeckBody returns deckID as reader reads it.
func readableDeckBody(t *testing.T, h *handlers.Handler, reader, deckID int64) *types.Deck {
	t.Helper()

	w := get(t, h.GetDeckByID, reader, deckRoute, deckPath(deckID))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read deck %d as %d: %d: %s", deckID, reader, w.Code, w.Body.String())
	}

	var deck types.Deck
	if err := json.Unmarshal(w.Body.Bytes(), &deck); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	return &deck
}
