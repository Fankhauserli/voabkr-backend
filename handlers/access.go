package handlers

import (
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

// requireReadableDeck loads a Deck and settles whether the caller may read it,
// resolving their Access role from the Deck and their own DeckShare. It answers
// the request itself: 404 when there is no such Deck, 403 when there is but the
// caller may not read it — ok is false once it has answered one of those.
//
// Every read of a Deck or its Cards goes through here, so the rule is applied in
// one place rather than re-spelled per handler.
func (h *Handler) requireReadableDeck(c *gin.Context, deckID int64, caller int64) (readableDeck, bool) {
	deck, err := h.DB.GetDeck(c.Request.Context(), deckID)
	if err != nil {
		respondWithError(c, http.StatusNotFound, "Deck not found", err)
		return readableDeck{}, false
	}

	var share *db.DeckShare
	if caller != access.Anonymous && !access.Owns(deck, caller) {
		if s, err := h.DB.GetDeckShare(c.Request.Context(), db.GetDeckShareParams{
			DeckID: deck.ID,
			UserID: pgtype.Int8{Int64: caller, Valid: true},
		}); err == nil {
			share = &s
		}
	}

	role := access.Role(deck, share, caller)
	if !access.CanRead(deck, role) {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access to this private deck is restricted"})
		return readableDeck{}, false
	}

	return readableDeck{deck: deck, role: role}, true
}

// callerParam carries a caller id into a query that filters by access. An
// anonymous caller is NULL, which matches no owner and no DeckShare.
func callerParam(caller int64) pgtype.Int8 {
	return pgtype.Int8{Int64: caller, Valid: caller != access.Anonymous}
}
