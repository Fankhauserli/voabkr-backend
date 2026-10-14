package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
)

// callerID returns the id of the learner making this request. ok is false for an
// anonymous request: one carrying no session user id, or one that does not parse.
func callerID(c *gin.Context) (id int64, ok bool) {
	raw, ok := sessions.Default(c).Get("user_id").(string)
	if !ok {
		return access.Anonymous, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return access.Anonymous, false
	}
	return id, true
}

// readableDeck is a Deck the caller may read, with the Access role they read it
// under.
type readableDeck struct {
	deck db.Deck
	role string
}

// How settling a Deck read can end without the caller being allowed in. Neither
// answers the request: the words a refusal is put in belong to whoever asked.
var (
	errNoSuchDeck      = errors.New("no such Deck")
	errDeckNotReadable = errors.New("Deck not readable by this caller")
)

// deckAccess settles whether caller may read deckID, returning the Deck and the
// Access role they read it under, without answering the request. A batch needs
// this: it refuses one entry at a time, so the refusal cannot be the request's.
func (h *Handler) deckAccess(ctx context.Context, deckID, caller int64) (readableDeck, error) {
	deck, err := h.DB.GetDeck(ctx, deckID)
	if err != nil {
		return readableDeck{}, fmt.Errorf("%w: %v", errNoSuchDeck, err)
	}

	var share *db.DeckShare
	if caller != access.Anonymous && !access.Owns(deck, caller) {
		if s, err := h.DB.GetDeckShare(ctx, db.GetDeckShareParams{
			DeckID: deck.ID,
			UserID: pgtype.Int8{Int64: caller, Valid: true},
		}); err == nil {
			share = &s
		}
	}

	role := access.Role(deck, share, caller)
	if !access.CanRead(deck, role) {
		return readableDeck{deck: deck, role: role}, errDeckNotReadable
	}

	return readableDeck{deck: deck, role: role}, nil
}

// requireReadableDeck loads a Deck and settles whether the caller may read it,
// resolving their Access role from the Deck and their own DeckShare. It answers
// the request itself: 404 when there is no such Deck, 403 when there is but the
// caller may not read it — ok is false once it has answered one of those.
//
// Every read of a Deck or its Cards goes through here, so the rule is applied in
// one place rather than re-spelled per handler.
func (h *Handler) requireReadableDeck(c *gin.Context, deckID int64, caller int64) (readableDeck, bool) {
	readable, err := h.deckAccess(c.Request.Context(), deckID, caller)
	switch {
	case errors.Is(err, errNoSuchDeck):
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return readableDeck{}, false
	case err != nil:
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
		return readableDeck{}, false
	}

	return readable, true
}

// callerParam carries a caller id into a query that filters by access. An
// anonymous caller is NULL, which matches no owner and no DeckShare.
func callerParam(caller int64) pgtype.Int8 {
	return pgtype.Int8{Int64: caller, Valid: caller != access.Anonymous}
}
