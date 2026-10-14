package handlers_test

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/handlers"
)

// cardWriteBody is a Card as the write routes take it.
func cardWriteBody(deckID int64, korean, english string) string {
	return fmt.Sprintf(`{"deckId":%d,"koreanWord":%q,"englishWord":%q}`, deckID, korean, english)
}

// deckCardWords returns the words of deckID's Cards as reader sees them — what a
// refused write left behind, read back through the interface a learner uses.
// reader must be able to read the Deck, so a System deck is read as anyone.
func deckCardWords(t *testing.T, h *handlers.Handler, reader, deckID int64) []string {
	t.Helper()

	w := get(t, h.GetCards, reader, cardsRoute, cardsPath(deckID))
	if w.Code != http.StatusOK {
		t.Fatalf("failed to read the deck's cards as %d: %d: %s", reader, w.Code, w.Body.String())
	}
	return cardWords(t, w.Body.Bytes())
}

// A learner with no standing on a Deck cannot add a Card to it, and the refusal
// leaves the Deck as it was.
func TestCreateCardInADeckTheCallerMayNotEditForbidden(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	stranger := seedUser(t, "stranger@example.com")
	deckID := seedDeck(t, owner, false)

	w := post(t, h.CreateCards, stranger, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a learner with no standing, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, deckID); len(got) != 0 {
		t.Fatalf("expected the refused write to store nothing, got cards %v", got)
	}
}

// The owner of a Deck adds a Card to it.
func TestCreateCardInOwnDeckByOwner(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	deckID := seedDeck(t, owner, false)

	w := post(t, h.CreateCards, owner, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 for the owner, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the created card in the deck, got %v", got)
	}
}

// A learner holding an edit Permission adds a Card to the shared Deck, and one
// holding a view Permission does not.
func TestCreateCardUnderSharePermission(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		want       int
		wantWords  []string
	}{
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusCreated, wantWords: []string{"나무"}},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := seedUser(t, "owner@example.com")
			learner := seedUser(t, "learner@example.com")
			deckID := seedDeck(t, owner, false)
			seedShare(t, deckID, learner, tt.permission)

			w := post(t, h.CreateCards, learner, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, tt.wantWords) {
				t.Fatalf("expected cards %v, got %v", tt.wantWords, got)
			}
		})
	}
}

// A System deck takes no Card write from a learner, whatever standing they hold
// on it: `owner_id IS NULL` means maintained content, never a deck to edit.
func TestCreateCardInSystemDeckForbidden(t *testing.T) {
	h := newTestHandler(t)
	learner := seedUser(t, "learner@example.com")
	deckID := seedDeck(t, 0, true)
	seedShare(t, deckID, learner, access.RoleEdit)

	w := post(t, h.CreateCards, learner, cardsRoute, cardsRoute, cardWriteBody(deckID, "나무", "tree"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, learner, deckID); len(got) != 0 {
		t.Fatalf("expected the refused write to store nothing, got cards %v", got)
	}
}

// A learner with no standing cannot change a Card they know the id of: the Deck
// the Card is stored in decides, and the refusal leaves the Card as it was.
func TestUpdateCardInADeckTheCallerMayNotEditForbidden(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	stranger := seedUser(t, "stranger@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")

	w := put(t, h.UpdateCard, stranger, cardRoute, cardPath(cardID), cardWriteBody(deckID, "바다", "sea"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a learner with no standing, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the refused write to leave the card alone, got %v", got)
	}
}

// The Deck a Card is stored in decides, never the one a request claims: owning
// another Deck does not make someone else's Card writable, and the claimed Deck
// gains nothing.
func TestUpdateCardSettlesTheStoredCardsDeckNotTheClaimedOne(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	other := seedUser(t, "other@example.com")
	deckID := seedDeck(t, owner, false)
	cardID := seedCard(t, deckID, "나무", "tree")
	otherDeckID := seedDeck(t, other, false)

	w := put(t, h.UpdateCard, other, cardRoute, cardPath(cardID), cardWriteBody(otherDeckID, "바다", "sea"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a learner claiming their own deck, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the card to stay in the deck that holds it, got %v", got)
	}
	if got := deckCardWords(t, h, other, otherDeckID); len(got) != 0 {
		t.Fatalf("expected the claimed deck to stay empty, got %v", got)
	}
}

// An edit of a Card cannot move it into a Deck the caller may not edit, even
// where they may edit the Deck the Card is stored in.
func TestUpdateCardCannotMoveIntoADeckTheCallerMayNotEdit(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	learner := seedUser(t, "learner@example.com")
	fromDeckID := seedDeck(t, owner, false)
	targetDeckID := seedDeck(t, owner, false)
	cardID := seedCard(t, fromDeckID, "나무", "tree")
	seedShare(t, fromDeckID, learner, access.RoleEdit)

	w := put(t, h.UpdateCard, learner, cardRoute, cardPath(cardID), cardWriteBody(targetDeckID, "바다", "sea"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a move into a deck the caller may not edit, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, fromDeckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the card to stay where it was, got %v", got)
	}
	if got := deckCardWords(t, h, owner, targetDeckID); len(got) != 0 {
		t.Fatalf("expected the target deck to stay empty, got %v", got)
	}
}

// Positive control for the refusal above: the same learner, holding an edit
// Permission on both Decks, moves the Card between them.
func TestUpdateCardMovesBetweenDecksTheCallerMayEdit(t *testing.T) {
	h := newTestHandler(t)
	owner := seedUser(t, "owner@example.com")
	learner := seedUser(t, "learner@example.com")
	fromDeckID := seedDeck(t, owner, false)
	targetDeckID := seedDeck(t, owner, false)
	cardID := seedCard(t, fromDeckID, "나무", "tree")
	seedShare(t, fromDeckID, learner, access.RoleEdit)
	seedShare(t, targetDeckID, learner, access.RoleEdit)

	w := put(t, h.UpdateCard, learner, cardRoute, cardPath(cardID), cardWriteBody(targetDeckID, "바다", "sea"))

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202 for a move between decks the caller may edit, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, owner, fromDeckID); len(got) != 0 {
		t.Fatalf("expected the card to have left its old deck, got %v", got)
	}
	if got := deckCardWords(t, h, owner, targetDeckID); !slices.Equal(got, []string{"바다"}) {
		t.Fatalf("expected the edited card in its new deck, got %v", got)
	}
}

// The owner of a Deck changes its Cards, and a learner with no standing cannot.
func TestUpdateCardUnderSharePermission(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		asOwner    bool
		want       int
		wantWords  []string
	}{
		{name: "owner", asOwner: true, want: http.StatusAccepted, wantWords: []string{"바다"}},
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusAccepted, wantWords: []string{"바다"}},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden, wantWords: []string{"나무"}},
		{name: "no standing", want: http.StatusForbidden, wantWords: []string{"나무"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := seedUser(t, "owner@example.com")
			caller := seedUser(t, "learner@example.com")
			if tt.asOwner {
				caller = owner
			}
			deckID := seedDeck(t, owner, false)
			cardID := seedCard(t, deckID, "나무", "tree")
			if tt.permission != "" {
				seedShare(t, deckID, caller, tt.permission)
			}

			w := put(t, h.UpdateCard, caller, cardRoute, cardPath(cardID), cardWriteBody(deckID, "바다", "sea"))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, tt.wantWords) {
				t.Fatalf("expected cards %v, got %v", tt.wantWords, got)
			}
		})
	}
}

// A System deck's Cards are not edited by a learner holding a share on it.
func TestUpdateCardInSystemDeckForbidden(t *testing.T) {
	h := newTestHandler(t)
	learner := seedUser(t, "learner@example.com")
	deckID := seedDeck(t, 0, true)
	cardID := seedCard(t, deckID, "나무", "tree")
	seedShare(t, deckID, learner, access.RoleEdit)

	w := put(t, h.UpdateCard, learner, cardRoute, cardPath(cardID), cardWriteBody(deckID, "바다", "sea"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, learner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the refused write to leave the card alone, got %v", got)
	}
}

// The owner of a Deck deletes its Cards, a learner with no standing does not, and
// a refusal leaves the Card in the Deck.
func TestDeleteCardUnderSharedStanding(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		asOwner    bool
		want       int
		wantWords  []string
	}{
		{name: "owner", asOwner: true, want: http.StatusAccepted},
		{name: "edit holder", permission: access.RoleEdit, want: http.StatusAccepted},
		{name: "view holder", permission: access.RoleView, want: http.StatusForbidden, wantWords: []string{"나무"}},
		{name: "no standing", want: http.StatusForbidden, wantWords: []string{"나무"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)
			owner := seedUser(t, "owner@example.com")
			caller := seedUser(t, "learner@example.com")
			if tt.asOwner {
				caller = owner
			}
			deckID := seedDeck(t, owner, false)
			cardID := seedCard(t, deckID, "나무", "tree")
			if tt.permission != "" {
				seedShare(t, deckID, caller, tt.permission)
			}

			w := del(t, h.DeleteCard, caller, cardRoute, cardPath(cardID))

			if w.Code != tt.want {
				t.Fatalf("expected status %d, got %d: %s", tt.want, w.Code, w.Body.String())
			}
			if got := deckCardWords(t, h, owner, deckID); !slices.Equal(got, tt.wantWords) {
				t.Fatalf("expected cards %v, got %v", tt.wantWords, got)
			}
		})
	}
}

// A System deck's Cards are not deleted by a learner holding a share on it.
func TestDeleteCardInSystemDeckForbidden(t *testing.T) {
	h := newTestHandler(t)
	learner := seedUser(t, "learner@example.com")
	deckID := seedDeck(t, 0, true)
	cardID := seedCard(t, deckID, "나무", "tree")
	seedShare(t, deckID, learner, access.RoleEdit)

	w := del(t, h.DeleteCard, learner, cardRoute, cardPath(cardID))

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for a System deck, got %d: %s", w.Code, w.Body.String())
	}
	if got := deckCardWords(t, h, learner, deckID); !slices.Equal(got, []string{"나무"}) {
		t.Fatalf("expected the refused delete to leave the card alone, got %v", got)
	}
}
